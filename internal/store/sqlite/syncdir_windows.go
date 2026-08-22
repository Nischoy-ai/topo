//go:build windows

package sqlite

// Windows does not provide the same portable directory-fsync operation as
// Unix. The completed database file is synced before publication; this no-op
// documents the strongest guarantee available through os.File on Windows.
func syncDirectory(string) error { return nil }
