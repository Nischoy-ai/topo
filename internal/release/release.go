// Package release builds the deterministic raw archives that are the source
// artifacts for Topo's later native packages and package-manager channels.
package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	projectName = "Nischoy Topo"
	repository  = "https://github.com/Nischoy-ai/topo"
)

var (
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	commitPattern  = regexp.MustCompile(`^(?:dev|[0-9a-f]{40,64})$`)
	fixedTime      = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	targets        = []Target{
		{GOOS: "darwin", GOARCH: "amd64", Format: "tar.gz"},
		{GOOS: "darwin", GOARCH: "arm64", Format: "tar.gz"},
		{GOOS: "linux", GOARCH: "amd64", Format: "tar.gz"},
		{GOOS: "linux", GOARCH: "arm64", Format: "tar.gz"},
		{GOOS: "windows", GOARCH: "amd64", Format: "zip"},
		{GOOS: "windows", GOARCH: "arm64", Format: "zip"},
	}
)

// Target is one released operating-system and architecture pair.
type Target struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	Format string `json:"format"`
}

// Options controls one release build. OutputDir must not already exist.
type Options struct {
	Root      string
	OutputDir string
	Version   string
	Commit    string
	GoBinary  string
}

type artifact struct {
	Filename string `json:"filename"`
	GOOS     string `json:"goos"`
	GOARCH   string `json:"goarch"`
	SHA256   string `json:"sha256"`
}

type metadata struct {
	SchemaVersion int        `json:"schema_version"`
	Project       string     `json:"project"`
	Repository    string     `json:"repository"`
	Version       string     `json:"version"`
	Commit        string     `json:"commit"`
	GoVersion     string     `json:"go_version"`
	CGOEnabled    bool       `json:"cgo_enabled"`
	BuildFlags    []string   `json:"build_flags"`
	Artifacts     []artifact `json:"artifacts"`
}

type archiveFile struct {
	Name string
	Path string
	Mode os.FileMode
}

// RearchiveDarwin creates a deterministic raw release archive from an already
// built and Developer-ID-signed macOS payload. It does not compile or modify
// the executable and is intended for the isolated native-signing job.
func RearchiveDarwin(inputDir, outputPath, version, arch string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("version %q must be a semantic tag such as v1.2.3", version)
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported Darwin architecture %q", arch)
	}
	inputDir, err := filepath.Abs(inputDir)
	if err != nil {
		return err
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("release output already exists: %s", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	base := fmt.Sprintf("topo_%s_darwin_%s", strings.TrimPrefix(version, "v"), arch)
	files := make([]archiveFile, 0, 3)
	for _, item := range []struct {
		name string
		mode os.FileMode
	}{{"LICENSE", 0o644}, {"README.md", 0o644}, {"topo", 0o755}} {
		path := filepath.Join(inputDir, item.name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect signed Darwin input %s: %w", item.name, err)
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 128<<20 {
			return fmt.Errorf("signed Darwin input %s is not a bounded regular file", item.name)
		}
		files = append(files, archiveFile{Name: base + "/" + item.name, Path: path, Mode: item.mode})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return writeTarGz(outputPath, files)
}

// RefreshMetadata updates raw-archive digests after isolated native signing.
// It preserves the recorded source, version, commit, and toolchain identity.
func RefreshMetadata(dir string) error {
	path := filepath.Join(dir, "release-metadata.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read release metadata: %w", err)
	}
	var manifest metadata
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return fmt.Errorf("parse release metadata: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Project != projectName || manifest.Repository != repository ||
		!versionPattern.MatchString(manifest.Version) || !commitPattern.MatchString(manifest.Commit) || len(manifest.Artifacts) != len(targets) {
		return errors.New("release metadata is incomplete or invalid")
	}
	expected := make(map[string]Target, len(targets))
	for _, target := range targets {
		name := fmt.Sprintf("topo_%s_%s_%s.%s", strings.TrimPrefix(manifest.Version, "v"), target.GOOS, target.GOARCH, target.Format)
		expected[name] = target
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for i := range manifest.Artifacts {
		item := &manifest.Artifacts[i]
		if filepath.Base(item.Filename) != item.Filename {
			return fmt.Errorf("unsafe release artifact filename %q", item.Filename)
		}
		if _, duplicate := seen[item.Filename]; duplicate {
			return fmt.Errorf("duplicate release artifact %s", item.Filename)
		}
		target, ok := expected[item.Filename]
		if !ok || item.GOOS != target.GOOS || item.GOARCH != target.GOARCH {
			return fmt.Errorf("release artifact identity mismatch for %s", item.Filename)
		}
		seen[item.Filename] = struct{}{}
		digest, err := fileSHA256(filepath.Join(dir, item.Filename))
		if err != nil {
			return fmt.Errorf("refresh %s: %w", item.Filename, err)
		}
		item.SHA256 = digest
	}
	updated, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, updated, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// Build compiles and archives every supported raw-binary target. The build
// excludes host paths, VCS probing, cgo, timestamps, uid/gid, and filesystem
// ordering as implicit inputs. Version and commit remain explicit inputs and
// are recorded in release-metadata.json.
func Build(ctx context.Context, options Options) (err error) {
	if err := validateOptions(&options); err != nil {
		return err
	}
	if _, err := os.Lstat(options.OutputDir); err == nil {
		return fmt.Errorf("release output already exists: %s", options.OutputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect release output: %w", err)
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create release output: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.RemoveAll(options.OutputDir)
		}
	}()

	work, err := os.MkdirTemp("", "topo-release-build-*")
	if err != nil {
		return fmt.Errorf("create release build directory: %w", err)
	}
	defer os.RemoveAll(work)

	goVersion, err := commandOutput(ctx, options.Root, options.GoBinary, "env", "GOVERSION")
	if err != nil {
		return err
	}
	versionName := strings.TrimPrefix(options.Version, "v")
	artifacts := make([]artifact, 0, len(targets))
	for _, target := range targets {
		base := fmt.Sprintf("topo_%s_%s_%s", versionName, target.GOOS, target.GOARCH)
		binaryName := "topo"
		if target.GOOS == "windows" {
			binaryName += ".exe"
		}
		binaryPath := filepath.Join(work, base, binaryName)
		if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}
		ldflags := "-s -w -X main.version=" + options.Version
		command := exec.CommandContext(ctx, options.GoBinary,
			"build", "-trimpath", "-buildvcs=false", "-ldflags="+ldflags,
			"-o", binaryPath, "./cmd/topo",
		)
		command.Dir = options.Root
		command.Env = buildEnvironment(target)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s/%s: %w: %s", target.GOOS, target.GOARCH, err, strings.TrimSpace(string(output)))
		}

		archiveName := base + "." + target.Format
		archivePath := filepath.Join(options.OutputDir, archiveName)
		files := []archiveFile{
			{Name: base + "/LICENSE", Path: filepath.Join(options.Root, "LICENSE"), Mode: 0o644},
			{Name: base + "/README.md", Path: filepath.Join(options.Root, "README.md"), Mode: 0o644},
			{Name: base + "/" + binaryName, Path: binaryPath, Mode: 0o755},
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		switch target.Format {
		case "tar.gz":
			err = writeTarGz(archivePath, files)
		case "zip":
			err = writeZip(archivePath, files)
		default:
			err = fmt.Errorf("unsupported archive format %q", target.Format)
		}
		if err != nil {
			return fmt.Errorf("archive %s: %w", archiveName, err)
		}
		digest, err := fileSHA256(archivePath)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact{Filename: archiveName, GOOS: target.GOOS, GOARCH: target.GOARCH, SHA256: digest})
	}

	manifest := metadata{
		SchemaVersion: 1,
		Project:       projectName,
		Repository:    repository,
		Version:       options.Version,
		Commit:        options.Commit,
		GoVersion:     goVersion,
		CGOEnabled:    false,
		BuildFlags:    []string{"-trimpath", "-buildvcs=false", "-ldflags=-s -w -X main.version=<version>"},
		Artifacts:     artifacts,
	}
	metadataBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal release metadata: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	metadataName := "release-metadata.json"
	if err := os.WriteFile(filepath.Join(options.OutputDir, metadataName), metadataBytes, 0o644); err != nil {
		return fmt.Errorf("write release metadata: %w", err)
	}
	checksumNames := make([]string, 0, len(artifacts)+1)
	for _, item := range artifacts {
		checksumNames = append(checksumNames, item.Filename)
	}
	checksumNames = append(checksumNames, metadataName)
	sort.Strings(checksumNames)
	var checksums strings.Builder
	for _, name := range checksumNames {
		digest, err := fileSHA256(filepath.Join(options.OutputDir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&checksums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "SHA256SUMS"), []byte(checksums.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	completed = true
	return nil
}

func validateOptions(options *Options) error {
	if options.Root == "" {
		options.Root = "."
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	options.Root = root
	if options.OutputDir == "" {
		return errors.New("release output directory is required")
	}
	output, err := filepath.Abs(options.OutputDir)
	if err != nil {
		return fmt.Errorf("resolve release output: %w", err)
	}
	options.OutputDir = output
	if !versionPattern.MatchString(options.Version) {
		return fmt.Errorf("version %q must be a semantic tag such as v1.2.3", options.Version)
	}
	if !commitPattern.MatchString(options.Commit) {
		return fmt.Errorf("commit %q must be dev or a 40-64 character lowercase hexadecimal digest", options.Commit)
	}
	if options.GoBinary == "" {
		options.GoBinary = "go"
	}
	for _, required := range []string{"go.mod", "LICENSE", "README.md"} {
		stat, err := os.Stat(filepath.Join(options.Root, required))
		if err != nil {
			return fmt.Errorf("release input %s: %w", required, err)
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("release input %s is not a regular file", required)
		}
	}
	return nil
}

func buildEnvironment(target Target) []string {
	replacements := map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        target.GOOS,
		"GOARCH":      target.GOARCH,
	}
	environment := make([]string, 0, len(os.Environ())+len(replacements))
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if _, replaced := replacements[name]; !replaced {
			environment = append(environment, item)
		}
	}
	for _, name := range []string{"CGO_ENABLED", "GOOS", "GOARCH"} {
		environment = append(environment, name+"="+replacements[name])
	}
	return environment
}

func commandOutput(ctx context.Context, root, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s %s: %w: %s", command, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func writeTarGz(path string, files []archiveFile) (err error) {
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = time.Time{}
	gzipWriter.Header.Name = ""
	gzipWriter.Header.Comment = ""
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		contents, err := os.Open(file.Path)
		if err != nil {
			return err
		}
		stat, err := contents.Stat()
		if err != nil {
			_ = contents.Close()
			return err
		}
		header := &tar.Header{
			Name:     filepath.ToSlash(file.Name),
			Mode:     int64(file.Mode.Perm()),
			Size:     stat.Size(),
			ModTime:  fixedTime,
			Typeflag: tar.TypeReg,
			Format:   tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			_ = contents.Close()
			return err
		}
		if _, err := io.Copy(tarWriter, contents); err != nil {
			_ = contents.Close()
			return err
		}
		if err := contents.Close(); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeZip(path string, files []archiveFile) (err error) {
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	zipWriter := zip.NewWriter(output)
	for _, file := range files {
		header := &zip.FileHeader{Name: filepath.ToSlash(file.Name), Method: zip.Deflate}
		header.SetMode(file.Mode)
		header.SetModTime(fixedTime)
		entry, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		contents, err := os.Open(file.Path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(entry, contents); err != nil {
			_ = contents.Close()
			return err
		}
		if err := contents.Close(); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for checksum: %w", filepath.Base(path), err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("checksum %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// RuntimeTarget reports the release tool's own platform for diagnostics.
func RuntimeTarget() string { return runtime.GOOS + "/" + runtime.GOARCH }
