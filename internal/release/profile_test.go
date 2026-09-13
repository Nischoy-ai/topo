package release

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseProfiles(t *testing.T) {
	for _, test := range []struct {
		profile, version string
		count            int
	}{
		{"", "v1.2.3", 6}, {"all", "v1.2.3-beta.1", 6},
		{LinuxMacOSBeta, "v1.2.3-beta.1", 4},
		{LinuxMacOSBeta, "v1.2.3", 0}, {"typo", "v1.2.3-beta.1", 0},
		{LinuxMacOSBeta, "not-a-version", 0},
	} {
		got, err := TargetsForProfile(test.profile, test.version)
		if (err != nil) != (test.count == 0) || len(got) != test.count {
			t.Fatalf("%q %q: targets=%v err=%v", test.profile, test.version, got, err)
		}
	}
}

func TestLinuxMacOSBuildAndRefresh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake compiler uses sh; real compiler is covered by CI")
	}
	root := t.TempDir()
	for _, name := range []string{"go.mod", "LICENSE", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	compiler := filepath.Join(root, "fake-go")
	script := `#!/bin/sh
set -eu
if [ "$1" = env ]; then echo go1.26.8; exit; fi
test "$1" = build
while [ "$1" != -o ]; do shift; done
shift
printf '%s' "$GOOS/$GOARCH-fixture" > "$1"
`
	if err := os.WriteFile(compiler, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	version := "v1.2.3-beta.1"
	out := filepath.Join(t.TempDir(), "out")
	if err := Build(context.Background(), Options{Root: root, OutputDir: out, Version: version, Commit: "dev", GoBinary: compiler, Profile: LinuxMacOSBeta}); err != nil {
		t.Fatal(err)
	}
	if err := RefreshMetadata(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "release-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest metadata
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Profile != LinuxMacOSBeta || len(manifest.Artifacts) != 4 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	for _, item := range manifest.Artifacts {
		if item.GOOS == "windows" || !strings.HasSuffix(item.Filename, ".tar.gz") {
			t.Fatal(item)
		}
	}
	if windows, err := ReadProfile(out, version); err != nil || windows {
		t.Fatalf("Windows=%v err=%v", windows, err)
	}
	if _, err := ReadProfile(out, "v1.2.3-beta.2"); err == nil {
		t.Fatal("accepted mismatched version")
	}
	path := filepath.Join(out, "unexpected.msi")
	if err := os.WriteFile(path, []byte("unsigned extra"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RefreshMetadata(out); err == nil {
		t.Fatal("refresh accepted Windows payload")
	}
	if _, err := ReadProfile(out, version); err == nil {
		t.Fatal("profile accepted Windows payload")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(out, manifest.Artifacts[0].Filename)); err != nil {
		t.Fatal(err)
	}
	if err := RefreshMetadata(out); err == nil {
		t.Fatal("refresh accepted missing selected archive")
	}
}
