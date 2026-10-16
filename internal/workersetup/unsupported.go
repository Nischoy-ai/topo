//go:build !linux

package workersetup

import "errors"

func platformBackend() (backend, error) {
	return nil, errors.New("worker setup requires packaged Linux with systemd; this platform is unsupported")
}
