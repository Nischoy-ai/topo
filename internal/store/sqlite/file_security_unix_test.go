//go:build !windows

package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSQLiteFilesRemainPrivateWithPermissiveUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	t.Run("live database and sidecars", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "topo.db")
		store, err := Open(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
			assertMode(t, path, 0o600)
		}
	})

	t.Run("published backup", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "topo.db")
		backupPath := filepath.Join(directory, "topo.backup.db")
		store, err := Open(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		if _, err := store.Backup(context.Background(), backupPath); err != nil {
			t.Fatal(err)
		}
		assertMode(t, backupPath, 0o600)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".topo-database-") {
				t.Fatalf("backup left private staging directory behind: %s", entry.Name())
			}
		}
	})

	t.Run("unpublished snapshot staging", func(t *testing.T) {
		directory := t.TempDir()
		destination := filepath.Join(directory, "topo.backup.db")
		temporary, cleanup, err := reserveTemporaryPath(destination)
		if err != nil {
			t.Fatal(err)
		}
		privateDirectory := filepath.Dir(temporary)
		defer cleanup()

		if privateDirectory == directory {
			t.Fatal("temporary snapshot path was reserved directly in the public destination directory")
		}
		assertMode(t, privateDirectory, 0o700)
		if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary snapshot path must remain absent until SQLite creates it: %v", err)
		}
		// VACUUM INTO chooses SQLite's default file mode before Backup can
		// chmod the completed snapshot. This intentionally permissive stand-in
		// proves that the entire creation window is still enclosed by a 0700
		// directory.
		if err := os.WriteFile(temporary, []byte("snapshot in progress"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertMode(t, temporary, 0o644)

		cleanup()
		if _, err := os.Lstat(privateDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("private staging directory survived cleanup: %v", err)
		}
	})
}

func TestOpenTightensExistingDatabasePermissions(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "topo.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertMode(t, databasePath, 0o600)
}

func TestOpenRejectsSymlinkDatabaseFiles(t *testing.T) {
	t.Run("main database", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target.db")
		if err := os.WriteFile(target, []byte("do not modify"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "topo.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}

		if _, err := Open(link); err == nil {
			t.Fatal("Open followed a symlink supplied as the database path")
		}
		assertMode(t, target, 0o644)
	})

	t.Run("WAL sidecar", func(t *testing.T) {
		directory := t.TempDir()
		databasePath := filepath.Join(directory, "topo.db")
		store, err := Open(databasePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(directory, "sidecar-target")
		if err := os.WriteFile(target, []byte("do not modify"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, databasePath+"-wal"); err != nil {
			t.Fatal(err)
		}

		if _, err := Open(databasePath); err == nil {
			t.Fatal("Open followed a symlink supplied as the WAL sidecar path")
		}
		assertMode(t, target, 0o644)
	})
}

func TestOpenRequiresFilesystemPath(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "topo.db")
	if _, err := Open("file:" + databasePath); err == nil {
		t.Fatal("Open accepted a SQLite URI instead of a filesystem path")
	}
	if _, err := os.Lstat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected SQLite URI created a database: %v", err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s = %04o, want %04o", path, got, want)
	}
}
