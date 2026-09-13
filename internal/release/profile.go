package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LinuxMacOSBeta is an explicit prerelease-only platform subset. Empty profile
// retains the historical full-platform contract, including for old metadata.
const LinuxMacOSBeta = "linux-macos-beta"

// TargetsForProfile returns a fresh, fixed target list; callers cannot supply
// arbitrary platforms or silently fall back when a profile is misspelled.
func TargetsForProfile(profile, version string) ([]Target, error) {
	if !versionPattern.MatchString(version) {
		return nil, errors.New("release profile requires a semantic version")
	}
	switch profile {
	case "", "all":
		return append([]Target(nil), targets...), nil
	case LinuxMacOSBeta:
		if !strings.Contains(version, "-") {
			return nil, errors.New("linux-macos-beta requires a prerelease version")
		}
		return append([]Target(nil), targets[:4]...), nil
	default:
		return nil, errors.New("unknown release profile")
	}
}

// CheckProfileFiles rejects Windows payloads rather than letting glob-based
// checksum, offline-bundle, or publication steps ship an unsigned extra file.
func CheckProfileFiles(dir, profile, version string) error {
	if _, err := TargetsForProfile(profile, version); err != nil {
		return err
	}
	if profile != LinuxMacOSBeta {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, "windows") || strings.HasSuffix(name, ".msi") || strings.HasSuffix(name, ".exe") {
			return fmt.Errorf("Windows artifact is excluded by linux-macos-beta: %s", entry.Name())
		}
	}
	return nil
}

// ReadProfile validates the version/profile in a release directory and returns
// whether Windows is part of that release. Call only after authenticating the
// checksum manifest when consuming a downloaded release.
func ReadProfile(dir, version string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "release-metadata.json"))
	if err != nil {
		return false, err
	}
	var manifest metadata
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, err
	}
	if manifest.Version != version {
		return false, errors.New("release profile version mismatch")
	}
	if err := CheckProfileFiles(dir, manifest.Profile, version); err != nil {
		return false, err
	}
	return manifest.Profile != LinuxMacOSBeta, nil
}
