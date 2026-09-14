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

// LinuxHomebrewBeta explicitly defers Apple Developer ID/notarization for a
// CLI formula beta. It does not change either existing profile's trust policy.
const LinuxHomebrewBeta = "linux-homebrew-beta"

// Policy is selected by reviewed source or authenticated release metadata,
// never by secret availability. Unknown profiles and stable opt-outs fail closed.
type Policy struct {
	IncludeWindows      bool
	RequireAppleSigning bool
}

func PolicyForProfile(profile, version string) (Policy, error) {
	if _, err := TargetsForProfile(profile, version); err != nil {
		return Policy{}, err
	}
	return Policy{
		IncludeWindows:      profile == "" || profile == "all",
		RequireAppleSigning: profile != LinuxHomebrewBeta,
	}, nil
}

// TargetsForProfile returns a fresh, fixed target list; callers cannot supply
// arbitrary platforms or silently fall back when a profile is misspelled.
func TargetsForProfile(profile, version string) ([]Target, error) {
	if !versionPattern.MatchString(version) {
		return nil, errors.New("release profile requires a semantic version")
	}
	switch profile {
	case "", "all":
		return append([]Target(nil), targets...), nil
	case LinuxMacOSBeta, LinuxHomebrewBeta:
		if !strings.Contains(version, "-") {
			return nil, errors.New("restricted beta profile requires a prerelease version")
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
	if profile != LinuxMacOSBeta && profile != LinuxHomebrewBeta {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, "windows") || strings.HasSuffix(name, ".msi") || strings.HasSuffix(name, ".exe") {
			return fmt.Errorf("Windows artifact is excluded by %s: %s", profile, entry.Name())
		}
	}
	return nil
}

// ReadProfile validates the version/profile in a release directory and returns
// whether Windows is part of that release. Call only after authenticating the
// checksum manifest when consuming a downloaded release.
func ReadProfile(dir, version string) (bool, error) {
	policy, err := ReadPolicy(dir, version)
	return policy.IncludeWindows, err
}

// ReadPolicy must only consume downloaded metadata after authenticating the
// checksum manifest; an unauthenticated profile cannot select a weaker policy.
func ReadPolicy(dir, version string) (Policy, error) {
	data, err := os.ReadFile(filepath.Join(dir, "release-metadata.json"))
	if err != nil {
		return Policy{}, err
	}
	var manifest metadata
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Policy{}, err
	}
	if manifest.Version != version {
		return Policy{}, errors.New("release profile version mismatch")
	}
	if err := CheckProfileFiles(dir, manifest.Profile, version); err != nil {
		return Policy{}, err
	}
	return PolicyForProfile(manifest.Profile, version)
}
