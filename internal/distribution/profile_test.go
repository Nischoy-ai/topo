package distribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxMacOSPromotion(t *testing.T) {
	for _, profile := range []string{"linux-macos-beta", "linux-homebrew-beta"} {
		t.Run(profile, func(t *testing.T) { testBetaPromotion(t, profile) })
	}
}

func testBetaPromotion(t *testing.T, profile string) {
	const version = "v1.2.3-beta.1"
	var previous map[string][]byte
	for run := 0; run < 2; run++ {
		dir := filepath.Join(t.TempDir(), "artifacts")
		writeProfileFixture(t, dir, version, profile, "")
		out := filepath.Join(t.TempDir(), "out")
		if err := Build(validOptions(dir, out, version, "beta")); err != nil {
			t.Fatal(err)
		}
		files := readTree(t, out)
		if run == 1 && treeDifference(previous, files) != "" {
			t.Fatal("subset promotion is not deterministic")
		}
		previous = files
		for name, data := range files {
			if strings.Contains(name, "winget") || strings.Contains(string(data), "windows_") {
				t.Fatalf("Windows reference in %s", name)
			}
		}
		if !strings.Contains(string(files["promotion-metadata.json"]), `"release_profile": "`+profile+`"`) {
			t.Fatal("profile not recorded")
		}
		if len(files["homebrew/Formula/topo-beta.rb"]) == 0 || len(files["apt/dists/beta/Release"]) == 0 {
			t.Fatal("missing Linux/macOS channel")
		}
	}
}

func TestPromotionProfileFailsClosed(t *testing.T) {
	for _, profile := range []string{"linux-macos-beta", "linux-homebrew-beta"} {
		t.Run(profile, func(t *testing.T) { testPromotionProfileFailsClosed(t, profile) })
	}
}

func testPromotionProfileFailsClosed(t *testing.T, selectedProfile string) {
	for _, kind := range []string{"unknown", "stable", "missing-mac", "missing-linux", "extra-windows"} {
		t.Run(kind, func(t *testing.T) {
			version, profile, channel, omit := "v1.2.3-beta.1", selectedProfile, "beta", ""
			switch kind {
			case "unknown":
				profile = "typo"
			case "stable":
				version, channel = "v1.2.3", "stable"
			case "missing-mac":
				omit = "topo_1.2.3-beta.1_darwin_arm64.tar.gz"
			case "missing-linux":
				omit = "topo_1.2.3-beta.1_arm64.deb"
			}
			dir := filepath.Join(t.TempDir(), "artifacts")
			writeProfileFixture(t, dir, version, profile, omit)
			if kind == "extra-windows" {
				if err := os.WriteFile(filepath.Join(dir, "extra.msi"), []byte("unsigned"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			out := filepath.Join(t.TempDir(), "out")
			if err := Build(validOptions(dir, out, version, channel)); err == nil {
				t.Fatal("accepted invalid subset")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("failed promotion left partial output")
			}
		})
	}
}
