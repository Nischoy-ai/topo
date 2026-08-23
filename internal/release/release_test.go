package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRearchiveDarwinIsDeterministic(t *testing.T) {
	directory := t.TempDir()
	for name, contents := range map[string]string{
		"LICENSE": "license\n", "README.md": "readme\n", "topo": "signed-binary",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(t.TempDir(), "first.tar.gz")
	second := filepath.Join(t.TempDir(), "second.tar.gz")
	if err := RearchiveDarwin(directory, first, "v1.2.3", "arm64"); err != nil {
		t.Fatal(err)
	}
	if err := RearchiveDarwin(directory, second, "v1.2.3", "arm64"); err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("signed Darwin inputs did not rearchive deterministically")
	}
	files := readTarGz(t, first)
	if files["topo_1.2.3_darwin_arm64/topo"].Mode != 0o755 ||
		files["topo_1.2.3_darwin_arm64/topo"].Contents != "signed-binary" {
		t.Fatalf("unexpected signed archive contents: %#v", files)
	}
}

func TestRefreshMetadataUpdatesNativeSignedArchiveDigest(t *testing.T) {
	directory := t.TempDir()
	artifacts := make([]artifact, 0, len(targets))
	for _, target := range targets {
		extension := target.Format
		name := "topo_1.2.3_" + target.GOOS + "_" + target.GOARCH + "." + extension
		if err := os.WriteFile(filepath.Join(directory, name), []byte("signed:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, artifact{Filename: name, GOOS: target.GOOS, GOARCH: target.GOARCH, SHA256: "stale"})
	}
	manifest := metadata{SchemaVersion: 1, Project: projectName, Repository: repository, Version: "v1.2.3", Commit: "0123456789abcdef0123456789abcdef01234567", Artifacts: artifacts}
	contents, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(directory, "release-metadata.json"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RefreshMetadata(directory); err != nil {
		t.Fatal(err)
	}
	updatedBytes, _ := os.ReadFile(filepath.Join(directory, "release-metadata.json"))
	var updated metadata
	if err := json.Unmarshal(updatedBytes, &updated); err != nil {
		t.Fatal(err)
	}
	for _, item := range updated.Artifacts {
		if item.SHA256 == "stale" || len(item.SHA256) != 64 {
			t.Fatalf("digest was not refreshed for %s: %q", item.Filename, item.SHA256)
		}
	}
}

func TestDeterministicArchives(t *testing.T) {
	directory := t.TempDir()
	licensePath := filepath.Join(directory, "LICENSE")
	binaryPath := filepath.Join(directory, "topo")
	if err := os.WriteFile(licensePath, []byte("license\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary\x00contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []archiveFile{
		{Name: "topo_1.0.0_linux_amd64/LICENSE", Path: licensePath, Mode: 0o644},
		{Name: "topo_1.0.0_linux_amd64/topo", Path: binaryPath, Mode: 0o755},
	}

	for _, test := range []struct {
		name  string
		ext   string
		write func(string, []archiveFile) error
		read  func(*testing.T, string) map[string]archivedFile
	}{
		{name: "tar-gzip", ext: ".tar.gz", write: writeTarGz, read: readTarGz},
		{name: "zip", ext: ".zip", write: writeZip, read: readZip},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := filepath.Join(directory, "first"+test.ext)
			second := filepath.Join(directory, "second"+test.ext)
			if err := test.write(first, files); err != nil {
				t.Fatal(err)
			}
			if err := test.write(second, files); err != nil {
				t.Fatal(err)
			}
			firstBytes, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			secondBytes, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstBytes, secondBytes) {
				t.Fatal("identical inputs produced different archive bytes")
			}
			got := test.read(t, first)
			want := map[string]archivedFile{
				"topo_1.0.0_linux_amd64/LICENSE": {Mode: 0o644, Contents: "license\n"},
				"topo_1.0.0_linux_amd64/topo":    {Mode: 0o755, Contents: "binary\x00contents"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("archive contents = %#v, want %#v", got, want)
			}
		})
	}
}

func TestValidateOptions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"go.mod", "LICENSE", "README.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		version string
		commit  string
		wantErr bool
	}{
		{name: "release", version: "v1.2.3", commit: "0123456789abcdef0123456789abcdef01234567"},
		{name: "prerelease", version: "v1.2.3-rc.1", commit: "dev"},
		{name: "missing-v", version: "1.2.3", commit: "dev", wantErr: true},
		{name: "empty-prerelease-part", version: "v1.2.3-rc.", commit: "dev", wantErr: true},
		{name: "version-injection", version: "v1.2.3 -X bad", commit: "dev", wantErr: true},
		{name: "commit-injection", version: "v1.2.3", commit: "HEAD;whoami", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := Options{Root: root, OutputDir: filepath.Join(root, "dist-"+test.name), Version: test.version, Commit: test.commit}
			err := validateOptions(&options)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOptions() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

type archivedFile struct {
	Mode     os.FileMode
	Contents string
}

func readTarGz(t *testing.T, path string) map[string]archivedFile {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	result := map[string]archivedFile{}
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
		result[header.Name] = archivedFile{Mode: os.FileMode(header.Mode), Contents: string(contents)}
	}
	return result
}

func readZip(t *testing.T, path string) map[string]archivedFile {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := map[string]archivedFile{}
	for _, file := range reader.File {
		contents, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(contents)
		if err != nil {
			_ = contents.Close()
			t.Fatal(err)
		}
		if err := contents.Close(); err != nil {
			t.Fatal(err)
		}
		result[file.Name] = archivedFile{Mode: file.Mode().Perm(), Contents: string(body)}
	}
	return result
}
