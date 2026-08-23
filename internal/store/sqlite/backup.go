package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// BackupInfo describes a verified SQLite backup or restored database.
type BackupInfo struct {
	Path          string
	SchemaVersion int
}

// Backup creates a consistent, compact snapshot of the open database at a
// new destination. SQLite's VACUUM INTO reads one transactionally consistent
// snapshot, including committed WAL contents. The destination is never
// overwritten and is published only after quick_check succeeds.
func (s *Store) Backup(ctx context.Context, destination string) (BackupInfo, error) {
	destination, err := cleanFilePath(destination, "backup destination")
	if err != nil {
		return BackupInfo{}, err
	}
	temporary, cleanup, err := reserveTemporaryPath(destination)
	if err != nil {
		return BackupInfo{}, err
	}
	defer cleanup()

	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, temporary); err != nil {
		return BackupInfo{}, fmt.Errorf("create SQLite backup: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return BackupInfo{}, fmt.Errorf("protect SQLite backup: %w", err)
	}
	info, err := Inspect(ctx, temporary)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("verify SQLite backup: %w", err)
	}
	if err := syncFile(temporary); err != nil {
		return BackupInfo{}, fmt.Errorf("sync SQLite backup: %w", err)
	}
	if err := publishWithoutOverwrite(temporary, destination); err != nil {
		return BackupInfo{}, fmt.Errorf("publish SQLite backup: %w", err)
	}
	info.Path = destination
	return info, nil
}

// Restore verifies and copies a backup into a new database path. It never
// overwrites an existing database; operators retain the old file as their
// rollback point and switch -db-dsn only after the restored copy is verified.
func Restore(ctx context.Context, backup, destination string) (BackupInfo, error) {
	backup, err := cleanFilePath(backup, "backup source")
	if err != nil {
		return BackupInfo{}, err
	}
	destination, err = cleanFilePath(destination, "restore destination")
	if err != nil {
		return BackupInfo{}, err
	}
	if samePath(backup, destination) {
		return BackupInfo{}, errors.New("backup source and restore destination must differ")
	}
	info, err := Inspect(ctx, backup)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("verify SQLite backup: %w", err)
	}
	temporary, cleanup, err := reserveTemporaryPath(destination)
	if err != nil {
		return BackupInfo{}, err
	}
	defer cleanup()
	if err := copyFile(backup, temporary); err != nil {
		return BackupInfo{}, fmt.Errorf("copy SQLite backup: %w", err)
	}
	if _, err := Inspect(ctx, temporary); err != nil {
		return BackupInfo{}, fmt.Errorf("verify restored SQLite database: %w", err)
	}
	if err := syncFile(temporary); err != nil {
		return BackupInfo{}, fmt.Errorf("sync restored SQLite database: %w", err)
	}
	if err := publishWithoutOverwrite(temporary, destination); err != nil {
		return BackupInfo{}, fmt.Errorf("publish restored SQLite database: %w", err)
	}
	info.Path = destination
	return info, nil
}

// Inspect opens a SQLite file read-only, runs quick_check, and returns its
// schema version without applying migrations or otherwise changing the file.
func Inspect(ctx context.Context, path string) (BackupInfo, error) {
	path, err := cleanFilePath(path, "database path")
	if err != nil {
		return BackupInfo{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("stat database: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return BackupInfo{}, errors.New("database must be a regular file")
	}
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return BackupInfo{}, fmt.Errorf("open database read-only: %w", err)
	}
	defer db.Close()

	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return BackupInfo{}, fmt.Errorf("run SQLite quick_check: %w", err)
	}
	if result != "ok" {
		return BackupInfo{}, fmt.Errorf("SQLite quick_check failed: %s", result)
	}
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return BackupInfo{}, fmt.Errorf("read schema version: %w", err)
	}
	if version < 1 || version > schemaVersion {
		return BackupInfo{}, fmt.Errorf("unsupported database schema version %d (supported: 1-%d)", version, schemaVersion)
	}
	return BackupInfo{Path: path, SchemaVersion: version}, nil
}

func cleanFilePath(path, name string) (string, error) {
	if strings.TrimSpace(path) == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return "", fmt.Errorf("%s must be a filesystem path", name)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	return filepath.Clean(absolute), nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func reserveTemporaryPath(destination string) (string, func(), error) {
	if _, err := os.Lstat(destination); err == nil {
		return "", nil, fmt.Errorf("destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("inspect destination: %w", err)
	}
	// VACUUM INTO must receive an absent path, so Topo cannot chmod the
	// snapshot itself until SQLite finishes creating it. Put that entire
	// creation window behind a private directory instead.
	directory, err := os.MkdirTemp(filepath.Dir(destination), ".topo-database-*")
	if err != nil {
		return "", nil, fmt.Errorf("create private temporary database directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if err := os.Chmod(directory, 0o700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("protect private temporary database directory: %w", err)
	}
	return filepath.Join(directory, "database.db"), cleanup, nil
}

func publishWithoutOverwrite(temporary, destination string) error {
	// Hard-linking within the destination directory publishes the completed
	// file atomically and, unlike Rename on Unix, fails if destination exists.
	if err := os.Link(temporary, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination already exists: %s", destination)
		}
		return err
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	completed := false
	defer func() {
		_ = out.Close()
		if !completed {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	completed = true
	return nil
}

func syncFile(path string) error {
	// Open read-write because Windows FlushFileBuffers requires a writable
	// handle even though Sync itself does not modify the completed snapshot.
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
