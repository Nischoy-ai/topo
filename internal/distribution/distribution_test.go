package distribution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBuildIsDeterministicAndPreservesArtifacts(t *testing.T) {
	t.Parallel()
	timestamp := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	var first map[string][]byte
	for run := 0; run < 2; run++ {
		root := t.TempDir()
		artifacts := filepath.Join(root, "source", "release")
		fixture := writeFixture(t, artifacts, "v1.2.3")
		out := filepath.Join(root, "different", "absolute", "output")
		if err := Build(Options{
			ArtifactDir:       artifacts,
			OutputDir:         out,
			Version:           "v1.2.3",
			Channel:           "stable",
			ReleaseBaseURL:    "https://github.com/Nischoy-ai/topo",
			RepositoryBaseURL: "https://packages.nischoy.com/topo",
			PublishedAt:       timestamp,
		}); err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		files := readTree(t, out)
		if run == 0 {
			first = files
		} else if diff := treeDifference(first, files); diff != "" {
			t.Fatalf("distribution output was not deterministic: %s", diff)
		}

		debPath := "apt/pool/main/t/topo/topo_1.2.3_amd64.deb"
		if got := string(files[debPath]); got != string(fixture["topo_1.2.3_amd64.deb"]) {
			t.Fatalf("APT pool changed the release artifact: %q", got)
		}
		rpmPath := "rpm/stable/x86_64/Packages/t/topo-1.2.3-1.x86_64.rpm"
		if got := string(files[rpmPath]); got != string(fixture["topo-1.2.3-1.x86_64.rpm"]) {
			t.Fatalf("RPM repository changed the release artifact: %q", got)
		}
		if got := string(files["helm/topo-1.2.3.tgz"]); got != string(fixture["topo-1.2.3.tgz"]) {
			t.Fatal("OCI Helm input changed the release artifact")
		}
		packages := string(files["apt/dists/stable/main/binary-amd64/Packages"])
		digest := sha256.Sum256(fixture["topo_1.2.3_amd64.deb"])
		if !strings.Contains(packages, "SHA256: "+hex.EncodeToString(digest[:])) ||
			!strings.Contains(packages, "Filename: pool/main/t/topo/topo_1.2.3_amd64.deb") {
			t.Fatalf("APT index does not authenticate the promoted package:\n%s", packages)
		}
		formula := string(files["homebrew/Formula/topo.rb"])
		if !strings.Contains(formula, "releases/download/v1.2.3/topo_1.2.3_darwin_arm64.tar.gz") ||
			!strings.Contains(formula, `assert_equal "v1.2.3"`) {
			t.Fatalf("Homebrew formula does not reference the immutable release: %s", formula)
		}
		installer := string(files["winget/manifests/n/Nischoy/Topo/1.2.3/Nischoy.Topo.installer.yaml"])
		if !strings.Contains(installer, "InstallerType: msi") || !strings.Contains(installer, "Architecture: arm64") {
			t.Fatalf("WinGet installer manifest is incomplete: %s", installer)
		}
		repo := string(files["rpm/nischoy-topo-stable.repo"])
		if !strings.Contains(repo, "gpgkey=https://packages.nischoy.com/topo/keys/nischoy-topo-archive.asc") {
			t.Fatalf("RPM repository does not reference its armored scoped key: %s", repo)
		}
	}
}

func TestBuildRejectsTamperingAndRemovesPartialOutput(t *testing.T) {
	t.Parallel()
	artifacts := filepath.Join(t.TempDir(), "release")
	writeFixture(t, artifacts, "v1.2.3")
	if err := os.WriteFile(filepath.Join(artifacts, "topo_1.2.3_amd64.deb"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	err := Build(validOptions(artifacts, out, "v1.2.3", "stable"))
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Build() error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("failed build left output behind: %v", statErr)
	}
}

func TestBuildEnforcesChannelPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		version, channel, message string
	}{
		{"v1.2.3-beta.1", "stable", "stable promotion rejects prerelease"},
		{"v1.2.3", "beta", "beta promotion requires a prerelease"},
		{"v1.2.3", "nightly", "channel must be stable or beta"},
	} {
		t.Run(test.version+"/"+test.channel, func(t *testing.T) {
			err := Build(validOptions(t.TempDir(), filepath.Join(t.TempDir(), "out"), test.version, test.channel))
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Build() error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestBuildKeepsBetaHomebrewSeparateFromStable(t *testing.T) {
	t.Parallel()
	artifacts := filepath.Join(t.TempDir(), "release")
	writeFixture(t, artifacts, "v1.2.3-beta.1")
	out := filepath.Join(t.TempDir(), "out")
	if err := Build(validOptions(artifacts, out, "v1.2.3-beta.1", "beta")); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(out, "homebrew", "Formula", "topo-beta.rb"))
	if err != nil {
		t.Fatal(err)
	}
	formula := string(contents)
	if !strings.Contains(formula, "class TopoBeta < Formula") || !strings.Contains(formula, `conflicts_with "topo"`) {
		t.Fatalf("beta formula is not isolated from stable: %s", formula)
	}
	if _, err := os.Stat(filepath.Join(out, "homebrew", "Formula", "topo.rb")); !os.IsNotExist(err) {
		t.Fatalf("beta promotion wrote the stable formula: %v", err)
	}
}

func TestProductCodeMatchesMSIBuilder(t *testing.T) {
	t.Parallel()
	if got, want := productCode("v0.0.0-ci", "amd64"), "{71D0DFCA-50BC-5767-B6C4-97B486E45940}"; got != want {
		t.Fatalf("productCode() = %s, want %s", got, want)
	}
}

func writeFixture(t *testing.T, dir, version string) map[string][]byte {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := strings.TrimPrefix(version, "v")
	names := []string{
		"topo_" + plain + "_amd64.deb", "topo_" + plain + "_arm64.deb",
		"topo-" + plain + "-1.x86_64.rpm", "topo-" + plain + "-1.aarch64.rpm",
		"topo_" + plain + "_darwin_amd64.tar.gz", "topo_" + plain + "_darwin_arm64.tar.gz",
		"topo_" + plain + "_linux_amd64.tar.gz", "topo_" + plain + "_linux_arm64.tar.gz",
		"topo_" + plain + "_windows_amd64.msi", "topo_" + plain + "_windows_arm64.msi",
		"topo-" + plain + ".tgz",
	}
	files := make(map[string][]byte)
	for _, name := range names {
		files[name] = []byte("fixture:" + name + "\n")
	}
	metadata, err := json.Marshal(releaseMetadata{
		SchemaVersion: 1,
		Project:       "Nischoy Topo",
		Repository:    "https://github.com/Nischoy-ai/topo",
		Version:       version,
		Commit:        strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	files["release-metadata.json"] = append(metadata, '\n')
	var manifest strings.Builder
	sorted := make([]string, 0, len(files))
	for name := range files {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(files[name])
		manifest.WriteString(hex.EncodeToString(digest[:]) + "  " + name + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(manifest.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return files
}

func validOptions(artifacts, out, version, channel string) Options {
	return Options{
		ArtifactDir: artifacts, OutputDir: out, Version: version, Channel: channel,
		ReleaseBaseURL: "https://github.com/Nischoy-ai/topo", RepositoryBaseURL: "https://packages.nischoy.com/topo",
		PublishedAt: time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC),
	}
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func treeDifference(first, second map[string][]byte) string {
	if len(first) != len(second) {
		return "file counts differ"
	}
	for name, contents := range first {
		other, ok := second[name]
		if !ok {
			return "missing " + name
		}
		if string(contents) != string(other) {
			return "contents differ for " + name
		}
	}
	return ""
}
