package packagebuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestBuildIsDeterministicAndUsesVerifiedPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper is a POSIX shell script")
	}
	root := fixtureRoot(t)
	raw := fixtureRawRelease(t, "v1.2.3", []byte("verified-linux-binary"))
	nfpm := fakeNFPM(t)
	outputA := filepath.Join(t.TempDir(), "output-a")
	outputB := filepath.Join(t.TempDir(), "output-b")
	for _, output := range []string{outputA, outputB} {
		if err := Build(context.Background(), Options{
			Root: root, RawDir: raw, OutputDir: output, Version: "v1.2.3", NFPMBinary: nfpm,
		}); err != nil {
			t.Fatalf("Build() error = %v", err)
		}
	}

	assertDirectoriesEqual(t, outputA, outputB)
	for _, name := range []string{
		"topo_1.2.3_amd64.deb",
		"topo_1.2.3_arm64.deb",
		"topo-1.2.3-1.x86_64.rpm",
		"topo-1.2.3-1.aarch64.rpm",
	} {
		contents, err := os.ReadFile(filepath.Join(outputA, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(contents, []byte("verified-linux-binary")) {
			t.Fatalf("%s does not contain the verified payload", name)
		}
	}
	chart := readTarGz(t, filepath.Join(outputA, "topo-1.2.3.tgz"))
	if !bytes.Contains(chart["topo/Chart.yaml"], []byte("version: 1.2.3")) {
		t.Fatalf("Chart.yaml does not contain packaged version: %s", chart["topo/Chart.yaml"])
	}
	if !bytes.Contains(chart["topo/Chart.yaml"], []byte(`appVersion: "v1.2.3"`)) {
		t.Fatalf("Chart.yaml does not contain app version: %s", chart["topo/Chart.yaml"])
	}
}

func TestBuildRejectsTamperedRawArtifactAndCleansOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper is a POSIX shell script")
	}
	root := fixtureRoot(t)
	raw := fixtureRawRelease(t, "v1.2.3", []byte("verified-linux-binary"))
	archive := filepath.Join(raw, "topo_1.2.3_linux_amd64.tar.gz")
	if err := os.WriteFile(archive, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "output")
	err := Build(context.Background(), Options{
		Root: root, RawDir: raw, OutputDir: output, Version: "v1.2.3", NFPMBinary: fakeNFPM(t),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Build() error = %v, want checksum mismatch", err)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("partial output remains after failure: %v", statErr)
	}
}

func TestWriteOfflineBundleIsDeterministicAndSelfVerifying(t *testing.T) {
	root := fixtureRoot(t)
	artifacts := t.TempDir()
	for name, contents := range map[string]string{
		"topo_1.2.3_amd64.deb": "deb",
		"topo-1.2.3.tgz":       "chart",
		"SHA256SUMS":           "outer checksum\n",
	} {
		if err := os.WriteFile(filepath.Join(artifacts, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(t.TempDir(), "first.tar.gz")
	second := filepath.Join(t.TempDir(), "second.tar.gz")
	for _, output := range []string{first, second} {
		if err := WriteOfflineBundle(root, artifacts, output, "v1.2.3"); err != nil {
			t.Fatalf("WriteOfflineBundle() error = %v", err)
		}
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("offline bundles differ")
	}
	files := readTarGz(t, first)
	top := "topo_1.2.3_offline/"
	manifest := string(files[top+"OFFLINE-SHA256SUMS"])
	for name, contents := range files {
		if name == top+"OFFLINE-SHA256SUMS" {
			continue
		}
		digest := sha256.Sum256(contents)
		line := hex.EncodeToString(digest[:]) + "  " + strings.TrimPrefix(name, top)
		if !strings.Contains(manifest, line+"\n") {
			t.Fatalf("offline manifest omits %s", name)
		}
	}
}

func TestVerifyRawArtifactsRejectsUnsafeFilename(t *testing.T) {
	dir := fixtureRawRelease(t, "v1.2.3", []byte("binary"))
	line := strings.Repeat("0", 64) + "  ../escape\n"
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyRawArtifacts(dir, "v1.2.3"); err == nil || !strings.Contains(err.Error(), "invalid raw checksum") {
		t.Fatalf("verifyRawArtifacts() error = %v, want unsafe checksum rejection", err)
	}
}

func TestRefreshPackageMetadataIncludesSignedMSIAndOfflineBundle(t *testing.T) {
	dir := t.TempDir()
	release := releaseMetadata{Version: "v1.2.3", Commit: strings.Repeat("b", 40)}
	contents, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release-metadata.json"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	msi := filepath.Join(dir, "topo_1.2.3_windows_amd64.msi")
	if err := os.WriteFile(msi, []byte("signed-msi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(msi+".payload.sha256", []byte(strings.Repeat("a", 64)+"  topo.exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "topo_1.2.3_offline.tar.gz"), []byte("offline"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RefreshPackageMetadata(dir); err != nil {
		t.Fatalf("RefreshPackageMetadata() error = %v", err)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(dir, "package-metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata packageMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Assembler["wix"] != "6.0.2" || len(metadata.Artifacts) != 2 {
		t.Fatalf("unexpected package metadata: %+v", metadata)
	}
	if metadata.Artifacts[1].Format != "msi" || metadata.Artifacts[1].SourceBinarySHA256 != strings.Repeat("a", 64) {
		t.Fatalf("MSI payload binding missing: %+v", metadata.Artifacts)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"LICENSE":                             "license\n",
		"README.md":                           "readme\n",
		"docs/packages.md":                    "packages\n",
		"docs/releases.md":                    "releases\n",
		"docs/storage.md":                     "storage\n",
		"packaging/nfpm.yaml":                 "arch: \"${TOPO_PACKAGE_ARCH}\"\nversion: \"${TOPO_PACKAGE_VERSION}\"\ncontents:\n  - src: \"${TOPO_PACKAGE_BINARY}\"\n    dst: /usr/bin/topo\n",
		"packaging/helm/topo/Chart.yaml":      "apiVersion: v2\nname: topo\nversion: 0.0.0-dev\nappVersion: \"dev\"\n",
		"packaging/helm/topo/values.yaml":     "apiKeySecret:\n  name: \"\"\n",
		"packaging/helm/topo/templates/x.tpl": "{{ .Chart.Name }}\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func fixtureRawRelease(t *testing.T, version string, binary []byte) string {
	t.Helper()
	dir := t.TempDir()
	filenameVersion := strings.TrimPrefix(version, "v")
	var artifacts []releaseArtifact
	for _, target := range []struct{ goos, arch, extension string }{
		{"darwin", "amd64", "tar.gz"}, {"darwin", "arm64", "tar.gz"},
		{"linux", "amd64", "tar.gz"}, {"linux", "arm64", "tar.gz"},
		{"windows", "amd64", "zip"}, {"windows", "arm64", "zip"},
	} {
		name := "topo_" + filenameVersion + "_" + target.goos + "_" + target.arch + "." + target.extension
		path := filepath.Join(dir, name)
		if target.goos == "linux" {
			writeRawTarGz(t, path, "topo_"+filenameVersion+"_linux_"+target.arch+"/topo", binary)
		} else {
			if err := os.WriteFile(path, []byte(target.goos+target.arch), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		digest, err := fileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, releaseArtifact{Filename: name, GOOS: target.goos, GOARCH: target.arch, SHA256: digest})
	}
	metadata := releaseMetadata{Version: version, Commit: strings.Repeat("a", 40), Artifacts: artifacts}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := os.WriteFile(filepath.Join(dir, "release-metadata.json"), metadataBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, artifact := range artifacts {
		names = append(names, artifact.Filename)
	}
	names = append(names, "release-metadata.json")
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		digest, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeRawTarGz(t *testing.T, path, name string, contents []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func fakeNFPM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nfpm")
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
    echo "nfpm version 2.47.0"
    exit 0
fi
target=""
config=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "--target" ]; then
        shift
        target="$1"
    elif [ "$1" = "--config" ]; then
        shift
        config="$1"
    fi
    shift
done
binary=$(sed -n 's/^  - src: "\(.*\)"/\1/p' "$config" | head -n 1)
printf 'package\n' > "$target"
cat "$binary" >> "$target"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertDirectoriesEqual(t *testing.T, left, right string) {
	t.Helper()
	leftEntries, err := os.ReadDir(left)
	if err != nil {
		t.Fatal(err)
	}
	rightEntries, err := os.ReadDir(right)
	if err != nil {
		t.Fatal(err)
	}
	if len(leftEntries) != len(rightEntries) {
		t.Fatalf("directory entry counts differ: %d != %d", len(leftEntries), len(rightEntries))
	}
	for i, entry := range leftEntries {
		if entry.Name() != rightEntries[i].Name() {
			t.Fatalf("directory entries differ: %s != %s", entry.Name(), rightEntries[i].Name())
		}
		leftBytes, _ := os.ReadFile(filepath.Join(left, entry.Name()))
		rightBytes, _ := os.ReadFile(filepath.Join(right, entry.Name()))
		if !bytes.Equal(leftBytes, rightBytes) {
			t.Fatalf("%s differs", entry.Name())
		}
	}
}

func readTarGz(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := make(map[string][]byte)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = contents
	}
	return files
}
