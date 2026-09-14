package packagebuild

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinuxMacOSPackageAssembly(t *testing.T) {
	for _, profile := range []string{"linux-macos-beta", "linux-homebrew-beta"} {
		t.Run(profile, func(t *testing.T) { testBetaPackageAssembly(t, profile) })
	}
}

func testBetaPackageAssembly(t *testing.T, profile string) {
	if runtime.GOOS == "windows" {
		t.Skip("fake nFPM uses sh")
	}
	version := "v1.2.3-beta.1"
	raw := fixtureRawReleaseProfile(t, version, []byte("binary"), profile)
	out := filepath.Join(t.TempDir(), "out")
	if err := Build(context.Background(), Options{Root: fixtureRoot(t), RawDir: raw, OutputDir: out, Version: version, NFPMBinary: fakeNFPM(t)}); err != nil {
		t.Fatal(err)
	}
	if err := RefreshPackageMetadata(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "package-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest packageMetadata
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Profile != profile || len(manifest.Artifacts) != 5 {
		t.Fatalf("unexpected packages: %+v", manifest)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "windows") {
			t.Fatal("Windows artifact shipped")
		}
	}
	if err := os.WriteFile(filepath.Join(out, "unexpected.msi"), []byte("extra"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RefreshPackageMetadata(out); err == nil {
		t.Fatal("accepted MSI in restricted profile")
	}
}

func TestRawProfileRejectsInvalidSets(t *testing.T) {
	for _, profile := range []string{"linux-macos-beta", "linux-homebrew-beta"} {
		t.Run(profile, func(t *testing.T) { testRawProfileRejectsInvalidSets(t, profile) })
	}
}

func testRawProfileRejectsInvalidSets(t *testing.T, profile string) {
	for _, mutation := range []string{"stable", "unknown", "duplicate", "missing", "wrong-platform"} {
		t.Run(mutation, func(t *testing.T) {
			version := "v1.2.3-beta.1"
			dir := fixtureRawReleaseProfile(t, version, []byte("binary"), profile)
			path := filepath.Join(dir, "release-metadata.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var manifest releaseMetadata
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "stable":
				version = "v1.2.3"
				manifest.Version = version
			case "unknown":
				manifest.Profile = "typo"
			case "duplicate":
				manifest.Artifacts[0] = manifest.Artifacts[1]
			case "missing":
				manifest.Artifacts = manifest.Artifacts[:3]
			case "wrong-platform":
				manifest.Artifacts[0].GOOS = "windows"
			}
			data, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := FinalizeChecksums(dir); err != nil {
				t.Fatal(err)
			}
			if _, _, err := verifyRawArtifacts(dir, version); err == nil {
				t.Fatal("accepted invalid profile set")
			}
		})
	}
}
