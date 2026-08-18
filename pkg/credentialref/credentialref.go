// Package credentialref resolves bounded credential references without
// accepting credential values as command-line arguments.
package credentialref

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxReferenceBytes  = 4 << 10
	maxCredentialBytes = 1 << 20
)

// ErrUnavailable indicates that a referenced credential is not present.
// Callers may ignore this error only for an explicitly optional credential.
var ErrUnavailable = errors.New("credential reference is unavailable")

// Resolve reads an env: or file: credential reference. It returns the exact
// credential bytes and never includes them in an error.
func Resolve(reference string) ([]byte, error) {
	if reference == "" {
		return nil, errors.New("credential reference is empty")
	}
	if len(reference) > maxReferenceBytes {
		return nil, errors.New("credential reference exceeds 4096 bytes")
	}
	kind, location, ok := strings.Cut(reference, ":")
	if !ok || location == "" {
		return nil, errors.New("credential reference must use env:<name> or file:<absolute-path>")
	}

	switch kind {
	case "env":
		return resolveEnvironment(location)
	case "file":
		return resolveFile(location)
	default:
		return nil, fmt.Errorf("unsupported credential reference provider %q", kind)
	}
}

func resolveEnvironment(name string) ([]byte, error) {
	if !validEnvironmentName(name) {
		return nil, errors.New("environment credential reference has an invalid variable name")
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return nil, fmt.Errorf("%w: environment variable %q is not set", ErrUnavailable, name)
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential from environment variable %q exceeds 1048576 bytes", name)
	}
	return []byte(value), nil
}

func resolveFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("file credential reference must use an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: credential file %q does not exist", ErrUnavailable, path)
		}
		return nil, fmt.Errorf("inspect credential file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credential file %q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open credential file %q: %w", path, err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open credential file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credential file %q is not a regular file", path)
	}

	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read credential file %q: %w", path, err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("credential file %q is empty", path)
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential file %q exceeds 1048576 bytes", path)
	}
	return value, nil
}

func validEnvironmentName(name string) bool {
	for index, character := range name {
		if character > unicode.MaxASCII {
			return false
		}
		if index == 0 {
			if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return name != ""
}
