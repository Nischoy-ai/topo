//go:build windows

package mid

// Windows does not expose a portable directory fsync through os.File.Sync.
// The journal file itself is synchronized before use.
func syncPlatformDirectory(string) error { return nil }
