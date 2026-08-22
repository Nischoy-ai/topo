package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
