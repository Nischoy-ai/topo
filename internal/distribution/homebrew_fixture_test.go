package distribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomebrewFixture(t *testing.T) {
	const version = "v1.2.3-beta.1"
	for _, mutation := range []string{"", "tampered", "missing", "stable", "unknown", "exists"} {
		t.Run(mutation, func(t *testing.T) {
			v, profile, omit := version, "linux-homebrew-beta", ""
			switch mutation {
			case "missing":
				omit = "topo_1.2.3-beta.1_darwin_arm64.tar.gz"
			case "stable":
				v = "v1.2.3"
			case "unknown":
				profile = "typo"
			}
			dir, out := filepath.Join(t.TempDir(), "raw files"), filepath.Join(t.TempDir(), "out")
			writeProfileFixture(t, dir, v, profile, omit)
			if mutation == "tampered" {
				if err := os.WriteFile(filepath.Join(dir, "topo_1.2.3-beta.1_darwin_amd64.tar.gz"), []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "exists" {
				if err := os.Mkdir(out, 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := WriteHomebrewFixture(dir, out, v)
			if (err != nil) != (mutation != "") {
				t.Fatalf("error=%v", err)
			}
			if mutation != "" {
				return
			}
			data, err := os.ReadFile(filepath.Join(out, "homebrew/Formula/topo-beta.rb"))
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"file://", "raw%20files", "sha256", `bin.install "topo"`, "topo discover local"} {
				if !strings.Contains(string(data), required) {
					t.Fatalf("missing %q", required)
				}
			}
			if strings.Contains(string(data), "/releases/download/") {
				t.Fatal("fixture downloads public release")
			}
		})
	}
}
