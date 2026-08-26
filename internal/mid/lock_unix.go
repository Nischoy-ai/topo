//go:build !windows

package mid

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockPlatformFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrMIDLocked
	}
	if err != nil {
		return fmt.Errorf("lock MID identity: %w", err)
	}
	return nil
}

func unlockPlatformFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("unlock MID identity: %w", err)
	}
	return nil
}
