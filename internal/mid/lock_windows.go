//go:build windows

package mid

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lockPlatformFile(file *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrMIDLocked
	}
	if err != nil {
		return fmt.Errorf("lock MID identity: %w", err)
	}
	return nil
}

func unlockPlatformFile(file *os.File) error {
	var overlapped windows.Overlapped
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped); err != nil {
		return fmt.Errorf("unlock MID identity: %w", err)
	}
	return nil
}
