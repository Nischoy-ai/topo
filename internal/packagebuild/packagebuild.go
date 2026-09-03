// Package packagebuild assembles native and deployment packages from Topo's
// already-verified raw release archives. It never compiles application code.
package packagebuild

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/internal/servicenowpackage"
)

const (
	nfpmVersion = "2.47.0"
	// maxFileSize bounds every regular file this package reads or writes,
	// including the offline bundle — the largest single release artifact by
	// design, since it aggregates every platform archive plus DEB/RPM/MSI/
	// Helm payloads into one file for air-gapped installs. Matches
	// internal/distribution's maxArtifactSize, which already used 512 MiB
	// for the same artifact set one stage further down the release
	// pipeline; 128 MiB was tight even before Kubernetes discovery roughly
	// doubled each platform binary's size by linking in k8s.io/client-go's
	// generated API types, and would only get tighter as AWS/Azure
	// discovery add comparable weight.
	maxFileSize = 512 << 20
)

var (
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	fixedTime      = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Options controls DEB, RPM, and Helm artifact assembly.
type Options struct {
	Root       string
	RawDir     string
	OutputDir  string
	Version    string
	NFPMBinary string
}

type releaseMetadata struct {
	Version   string            `json:"version"`
	Commit    string            `json:"commit"`
	Artifacts []releaseArtifact `json:"artifacts"`
}

type releaseArtifact struct {
	Filename string `json:"filename"`
	GOOS     string `json:"goos"`
	GOARCH   string `json:"goarch"`
	SHA256   string `json:"sha256"`
}

type packageMetadata struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	Assembler     map[string]string `json:"assembler"`
	Artifacts     []packageArtifact `json:"artifacts"`
}

type packageArtifact struct {
	Filename           string `json:"filename"`
	Format             string `json:"format"`
	Architecture       string `json:"architecture,omitempty"`
	SourceArchive      string `json:"source_archive,omitempty"`
	SourceBinarySHA256 string `json:"source_binary_sha256,omitempty"`
	ArtifactSHA256     string `json:"artifact_sha256"`
}

type checksumEntry struct {
	Digest   string
	Filename string
}

type chartFile struct {
	Name string
	Path string
	Mode fs.FileMode
}

// Build verifies and copies the raw artifacts, then creates Linux native
// packages and the Helm chart from their payloads. OutputDir must not exist.
func Build(ctx context.Context, options Options) (err error) {
	if err := validateBuildOptions(&options); err != nil {
		return err
	}
	if err := requireAbsent(options.OutputDir); err != nil {
		return err
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create package output: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.RemoveAll(options.OutputDir)
		}
	}()

	metadata, checksums, err := verifyRawArtifacts(options.RawDir, options.Version)
	if err != nil {
		return err
	}
	for _, entry := range checksums {
		if err := copyRegularFile(
			filepath.Join(options.RawDir, entry.Filename),
			filepath.Join(options.OutputDir, entry.Filename),
		); err != nil {
			return err
		}
	}

	nfpmOutput, err := commandOutput(ctx, options.Root, options.NFPMBinary, "--version")
	if err != nil {
		return err
	}
	if !strings.Contains(nfpmOutput, nfpmVersion) {
		return fmt.Errorf("nFPM must be exactly %s, got %q", nfpmVersion, nfpmOutput)
	}

	work, err := os.MkdirTemp("", "topo-package-build-*")
	if err != nil {
		return fmt.Errorf("create package work directory: %w", err)
	}
	defer os.RemoveAll(work)

	packageArtifacts := make([]packageArtifact, 0, 5)
	filenameVersion := strings.TrimPrefix(options.Version, "v")
	for _, arch := range []string{"amd64", "arm64"} {
		rawName := fmt.Sprintf("topo_%s_linux_%s.tar.gz", filenameVersion, arch)
		binaryPath := filepath.Join(work, "linux-"+arch, "topo")
		if err := extractRawBinary(filepath.Join(options.RawDir, rawName), binaryPath, "topo"); err != nil {
			return fmt.Errorf("extract %s: %w", rawName, err)
		}
		binaryDigest, err := fileSHA256(binaryPath)
		if err != nil {
			return err
		}
		for _, format := range []string{"deb", "rpm"} {
			name := linuxPackageName(filenameVersion, arch, format)
			path := filepath.Join(options.OutputDir, name)
			if err := runNFPM(ctx, options, arch, binaryPath, format, path); err != nil {
				return err
			}
			packageDigest, err := fileSHA256(path)
			if err != nil {
				return err
			}
			if err := writePayloadChecksum(path, binaryDigest); err != nil {
				return err
			}
			packageArtifacts = append(packageArtifacts, packageArtifact{
				Filename:           name,
				Format:             format,
				Architecture:       arch,
				SourceArchive:      rawName,
				SourceBinarySHA256: binaryDigest,
				ArtifactSHA256:     packageDigest,
			})
		}
	}

	chartName := "topo-" + filenameVersion + ".tgz"
	chartPath := filepath.Join(options.OutputDir, chartName)
	if err := writeChart(filepath.Join(options.Root, "packaging", "helm", "topo"), chartPath, options.Version); err != nil {
		return err
	}
	chartDigest, err := fileSHA256(chartPath)
	if err != nil {
		return err
	}
	packageArtifacts = append(packageArtifacts, packageArtifact{
		Filename:       chartName,
		Format:         "helm",
		ArtifactSHA256: chartDigest,
	})

	if err := writePackageMetadata(options.OutputDir, packageMetadata{
		SchemaVersion: 1,
		Version:       options.Version,
		Commit:        metadata.Commit,
		Assembler: map[string]string{
			"nfpm": nfpmVersion,
		},
		Artifacts: packageArtifacts,
	}); err != nil {
		return err
	}
	if err := FinalizeChecksums(options.OutputDir); err != nil {
		return err
	}
	completed = true
	return nil
}

// FinalizeChecksums rewrites the release checksum manifest from every regular
// artifact in dir except the manifest itself and package payload sidecars.
func FinalizeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read artifact directory: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "SHA256SUMS" || strings.HasSuffix(entry.Name(), ".payload.sha256") {
			continue
		}
		if !safeBaseName(entry.Name()) {
			return fmt.Errorf("unsafe artifact filename %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact %s is not a regular file", entry.Name())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var manifest strings.Builder
	for _, name := range names {
		digest, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", digest, name)
	}
	path := filepath.Join(dir, "SHA256SUMS")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(manifest.String()), 0o644); err != nil {
		return fmt.Errorf("write checksum manifest: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish checksum manifest: %w", err)
	}
	return nil
}

// RefreshPackageMetadata reconstructs package-metadata.json from the complete
// cross-platform artifact set after platform jobs have converged.
func RefreshPackageMetadata(dir string) error {
	contents, err := os.ReadFile(filepath.Join(dir, "release-metadata.json"))
	if err != nil {
		return fmt.Errorf("read release metadata: %w", err)
	}
	var release releaseMetadata
	if err := json.Unmarshal(contents, &release); err != nil {
		return fmt.Errorf("parse release metadata: %w", err)
	}
	if !versionPattern.MatchString(release.Version) {
		return fmt.Errorf("release metadata has invalid version %q", release.Version)
	}
	filenameVersion := strings.TrimPrefix(release.Version, "v")
	metadata := packageMetadata{
		SchemaVersion: 1,
		Version:       release.Version,
		Commit:        release.Commit,
		Assembler:     map[string]string{"nfpm": nfpmVersion},
	}
	for _, item := range []struct {
		name, format, arch, source string
	}{
		{"topo_" + filenameVersion + "_amd64.deb", "deb", "amd64", "topo_" + filenameVersion + "_linux_amd64.tar.gz"},
		{"topo_" + filenameVersion + "_arm64.deb", "deb", "arm64", "topo_" + filenameVersion + "_linux_arm64.tar.gz"},
		{"topo-" + filenameVersion + "-1.x86_64.rpm", "rpm", "amd64", "topo_" + filenameVersion + "_linux_amd64.tar.gz"},
		{"topo-" + filenameVersion + "-1.aarch64.rpm", "rpm", "arm64", "topo_" + filenameVersion + "_linux_arm64.tar.gz"},
		{"topo_" + filenameVersion + "_windows_amd64.msi", "msi", "amd64", "topo_" + filenameVersion + "_windows_amd64.zip"},
		{"topo_" + filenameVersion + "_windows_arm64.msi", "msi", "arm64", "topo_" + filenameVersion + "_windows_arm64.zip"},
		{"topo-" + filenameVersion + ".tgz", "helm", "", ""},
		{"topo_" + filenameVersion + "_offline.tar.gz", "offline", "", ""},
	} {
		path := filepath.Join(dir, item.name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect package artifact %s: %w", item.name, err)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		artifact := packageArtifact{
			Filename:       item.name,
			Format:         item.format,
			Architecture:   item.arch,
			SourceArchive:  item.source,
			ArtifactSHA256: digest,
		}
		if item.format == "deb" || item.format == "rpm" || item.format == "msi" {
			payload, err := readPayloadChecksum(path + ".payload.sha256")
			if err != nil {
				return err
			}
			artifact.SourceBinarySHA256 = payload
		}
		if item.format == "msi" {
			metadata.Assembler["wix"] = "6.0.2"
		}
		metadata.Artifacts = append(metadata.Artifacts, artifact)
	}
	serviceNowPath := filepath.Join(dir, servicenowpackage.ArtifactName)
	if _, err := os.Stat(serviceNowPath); err == nil {
		metadataPath := filepath.Join(dir, "servicenow-app-metadata.json")
		contents, err := os.ReadFile(metadataPath)
		if err != nil {
			return fmt.Errorf("read ServiceNow app metadata: %w", err)
		}
		var app servicenowpackage.Metadata
		if err := json.Unmarshal(contents, &app); err != nil {
			return fmt.Errorf("parse ServiceNow app metadata: %w", err)
		}
		digest, err := fileSHA256(serviceNowPath)
		if err != nil {
			return err
		}
		if app.SchemaVersion != 1 || app.Scope != servicenowpackage.Scope || app.AppVersion != servicenowpackage.AppVersion || app.SDKVersion != servicenowpackage.SDKVersion || app.Artifact != servicenowpackage.ArtifactName || app.SHA256 != digest {
			return errors.New("ServiceNow app metadata does not match the validated release artifact")
		}
		metadata.Assembler["servicenow-sdk"] = servicenowpackage.SDKVersion
		metadata.Artifacts = append(metadata.Artifacts, packageArtifact{
			Filename: servicenowpackage.ArtifactName, Format: "servicenow-app", ArtifactSHA256: digest,
		})
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect ServiceNow app artifact: %w", err)
	} else if _, metadataErr := os.Stat(filepath.Join(dir, "servicenow-app-metadata.json")); metadataErr == nil {
		return errors.New("ServiceNow app metadata exists without its release artifact")
	} else if !errors.Is(metadataErr, os.ErrNotExist) {
		return fmt.Errorf("inspect ServiceNow app metadata: %w", metadataErr)
	}
	return writePackageMetadata(dir, metadata)
}

// WriteOfflineBundle creates one deterministic archive from the artifacts and
// selected operator documentation already present in the repository.
func WriteOfflineBundle(root, artifactDir, outputPath, version string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("version %q must be a semantic tag such as v1.2.3", version)
	}
	if err := requireAbsent(outputPath); err != nil {
		return err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	artifactDir, err = filepath.Abs(artifactDir)
	if err != nil {
		return fmt.Errorf("resolve artifact directory: %w", err)
	}

	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		return fmt.Errorf("read artifact directory: %w", err)
	}
	top := "topo_" + strings.TrimPrefix(version, "v") + "_offline"
	files := make([]chartFile, 0, len(entries)+6)
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".payload.sha256") || entry.Name() == filepath.Base(outputPath) {
			continue
		}
		if !safeBaseName(entry.Name()) {
			return fmt.Errorf("unsafe artifact filename %q", entry.Name())
		}
		files = append(files, chartFile{
			Name: top + "/artifacts/" + entry.Name(),
			Path: filepath.Join(artifactDir, entry.Name()),
			Mode: 0o644,
		})
	}
	for _, name := range []string{"LICENSE", "README.md", "docs/packages.md", "docs/pilot-quickstart.md", "docs/releases.md", "docs/storage.md"} {
		files = append(files, chartFile{
			Name: top + "/" + filepath.ToSlash(name),
			Path: filepath.Join(root, filepath.FromSlash(name)),
			Mode: 0o644,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	checksums, err := bundleChecksums(files, top)
	if err != nil {
		return err
	}
	return writeDeterministicTarGz(outputPath, files, top+"/OFFLINE-SHA256SUMS", checksums)
}

func validateBuildOptions(options *Options) error {
	if !versionPattern.MatchString(options.Version) {
		return fmt.Errorf("version %q must be a semantic tag such as v1.2.3", options.Version)
	}
	if options.NFPMBinary == "" {
		return errors.New("path to pinned nFPM binary is required")
	}
	for label, value := range map[string]*string{
		"repository root":          &options.Root,
		"raw artifact directory":   &options.RawDir,
		"package output directory": &options.OutputDir,
		"nFPM binary":              &options.NFPMBinary,
	} {
		if *value == "" {
			return fmt.Errorf("%s is required", label)
		}
		absolute, err := filepath.Abs(*value)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", label, err)
		}
		*value = absolute
	}
	return nil
}

func verifyRawArtifacts(dir, version string) (releaseMetadata, []checksumEntry, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "release-metadata.json"))
	if err != nil {
		return releaseMetadata{}, nil, fmt.Errorf("read release metadata: %w", err)
	}
	var metadata releaseMetadata
	if err := json.Unmarshal(manifestBytes, &metadata); err != nil {
		return releaseMetadata{}, nil, fmt.Errorf("parse release metadata: %w", err)
	}
	if metadata.Version != version {
		return releaseMetadata{}, nil, fmt.Errorf("release metadata version %q does not match %q", metadata.Version, version)
	}
	if len(metadata.Artifacts) != 6 {
		return releaseMetadata{}, nil, fmt.Errorf("release metadata must contain six raw artifacts, got %d", len(metadata.Artifacts))
	}

	checksumFile, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return releaseMetadata{}, nil, fmt.Errorf("open raw checksum manifest: %w", err)
	}
	defer checksumFile.Close()
	var entries []checksumEntry
	seen := make(map[string]string)
	scanner := bufio.NewScanner(io.LimitReader(checksumFile, 1<<20))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "  ", 2)
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 || !safeBaseName(parts[1]) {
			return releaseMetadata{}, nil, fmt.Errorf("invalid raw checksum line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return releaseMetadata{}, nil, fmt.Errorf("invalid digest for %s", parts[1])
		}
		if _, duplicate := seen[parts[1]]; duplicate {
			return releaseMetadata{}, nil, fmt.Errorf("duplicate checksum entry %s", parts[1])
		}
		actual, err := fileSHA256(filepath.Join(dir, parts[1]))
		if err != nil {
			return releaseMetadata{}, nil, err
		}
		if actual != parts[0] {
			return releaseMetadata{}, nil, fmt.Errorf("checksum mismatch for %s", parts[1])
		}
		seen[parts[1]] = parts[0]
		entries = append(entries, checksumEntry{Digest: parts[0], Filename: parts[1]})
	}
	if err := scanner.Err(); err != nil {
		return releaseMetadata{}, nil, fmt.Errorf("read raw checksum manifest: %w", err)
	}
	for _, artifact := range metadata.Artifacts {
		if seen[artifact.Filename] != artifact.SHA256 {
			return releaseMetadata{}, nil, fmt.Errorf("metadata/checksum disagreement for %s", artifact.Filename)
		}
	}
	if _, ok := seen["release-metadata.json"]; !ok {
		return releaseMetadata{}, nil, errors.New("raw checksums omit release-metadata.json")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Filename < entries[j].Filename })
	return metadata, entries, nil
}

func extractRawBinary(archivePath, outputPath, binaryName string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(io.LimitReader(archive, maxFileSize))
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	found := false
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(header.Name)
		if pathUnsafe(name) {
			return fmt.Errorf("unsafe archive entry %q", name)
		}
		if filepath.Base(name) != binaryName {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > maxFileSize {
			return fmt.Errorf("invalid duplicate or non-regular %s payload", binaryName)
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(output, tarReader, header.Size)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		found = true
	}
	if !found {
		return fmt.Errorf("archive does not contain %s", binaryName)
	}
	return nil
}

func runNFPM(ctx context.Context, options Options, arch, binary, format, output string) error {
	templatePath := filepath.Join(options.Root, "packaging", "nfpm.yaml")
	configuration, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read nFPM configuration: %w", err)
	}
	replacements := map[string]string{
		`"${TOPO_PACKAGE_ARCH}"`:    strconv.Quote(arch),
		`"${TOPO_PACKAGE_VERSION}"`: strconv.Quote(strings.TrimPrefix(options.Version, "v")),
		`"${TOPO_PACKAGE_BINARY}"`:  strconv.Quote(binary),
	}
	configured := string(configuration)
	for placeholder, value := range replacements {
		if !strings.Contains(configured, placeholder) {
			return fmt.Errorf("nFPM configuration omits placeholder %s", placeholder)
		}
		configured = strings.ReplaceAll(configured, placeholder, value)
	}
	configFile, err := os.CreateTemp(filepath.Dir(binary), "nfpm-*.yaml")
	if err != nil {
		return fmt.Errorf("create nFPM configuration: %w", err)
	}
	configPath := configFile.Name()
	defer os.Remove(configPath)
	if _, err := configFile.WriteString(configured); err != nil {
		_ = configFile.Close()
		return fmt.Errorf("write nFPM configuration: %w", err)
	}
	if err := configFile.Close(); err != nil {
		return fmt.Errorf("close nFPM configuration: %w", err)
	}

	command := exec.CommandContext(ctx, options.NFPMBinary,
		"package", "--config", configPath,
		"--packager", format, "--target", output,
	)
	command.Dir = options.Root
	command.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=315532800")
	if outputBytes, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s/%s package: %w: %s", format, arch, err, strings.TrimSpace(string(outputBytes)))
	}
	return nil
}

func linuxPackageName(version, arch, format string) string {
	if format == "rpm" {
		rpmArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[arch]
		return fmt.Sprintf("topo-%s-1.%s.rpm", version, rpmArch)
	}
	return fmt.Sprintf("topo_%s_%s.deb", version, arch)
}

func writeChart(sourceDir, outputPath, version string) error {
	var files []chartFile
	err := filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("chart input %s is not a regular file", path)
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		files = append(files, chartFile{Name: "topo/" + filepath.ToSlash(relative), Path: path, Mode: 0o644})
		return nil
	})
	if err != nil {
		return fmt.Errorf("enumerate Helm chart: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return writeChartTarGz(outputPath, files, version)
}

func writeChartTarGz(path string, files []chartFile, version string) (err error) {
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
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		contents, err := os.ReadFile(file.Path)
		if err != nil {
			return err
		}
		if file.Name == "topo/Chart.yaml" {
			contents = []byte(strings.ReplaceAll(string(contents), "0.0.0-dev", strings.TrimPrefix(version, "v")))
			contents = []byte(strings.ReplaceAll(string(contents), `appVersion: "dev"`, `appVersion: "`+version+`"`))
		}
		if err := writeTarEntry(tarWriter, file.Name, file.Mode, contents); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func bundleChecksums(files []chartFile, top string) ([]byte, error) {
	var manifest strings.Builder
	for _, file := range files {
		digest, err := fileSHA256(file.Path)
		if err != nil {
			return nil, err
		}
		relative := strings.TrimPrefix(file.Name, top+"/")
		fmt.Fprintf(&manifest, "%s  %s\n", digest, relative)
	}
	return []byte(manifest.String()), nil
}

func writeDeterministicTarGz(path string, files []chartFile, extraName string, extra []byte) (err error) {
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
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		contents, err := os.ReadFile(file.Path)
		if err != nil {
			return err
		}
		if err := writeTarEntry(tarWriter, file.Name, file.Mode, contents); err != nil {
			return err
		}
	}
	if err := writeTarEntry(tarWriter, extraName, 0o644, extra); err != nil {
		return err
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeTarEntry(writer *tar.Writer, name string, mode fs.FileMode, contents []byte) error {
	header := &tar.Header{
		Name:     filepath.ToSlash(name),
		Mode:     int64(mode.Perm()),
		Size:     int64(len(contents)),
		ModTime:  fixedTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatUSTAR,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(contents)
	return err
}

func writePackageMetadata(dir string, metadata packageMetadata) error {
	sort.Slice(metadata.Artifacts, func(i, j int) bool { return metadata.Artifacts[i].Filename < metadata.Artifacts[j].Filename })
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal package metadata: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(filepath.Join(dir, "package-metadata.json"), contents, 0o644); err != nil {
		return fmt.Errorf("write package metadata: %w", err)
	}
	return nil
}

func writePayloadChecksum(packagePath, digest string) error {
	contents := []byte(digest + "  topo\n")
	if err := os.WriteFile(packagePath+".payload.sha256", contents, 0o644); err != nil {
		return fmt.Errorf("write package payload checksum: %w", err)
	}
	return nil
}

func readPayloadChecksum(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read package payload checksum: %w", err)
	}
	fields := strings.Fields(string(contents))
	if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || fields[1] != "topo" && fields[1] != "topo.exe" {
		return "", fmt.Errorf("invalid package payload checksum %s", filepath.Base(path))
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", fmt.Errorf("invalid package payload digest %s", filepath.Base(path))
	}
	return fields[0], nil
}

func copyRegularFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return fmt.Errorf("input %s is not a bounded regular file", filepath.Base(source))
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(destination), err)
	}
	_, copyErr := io.Copy(output, io.LimitReader(input, maxFileSize+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func commandOutput(ctx context.Context, root, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run %s: %w: %s", filepath.Base(command), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func requireAbsent(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("output already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output: %w", err)
	}
	return nil
}

func safeBaseName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func pathUnsafe(name string) bool {
	clean := filepath.ToSlash(filepath.Clean(name))
	return clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(name, `\`)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for checksum: %w", filepath.Base(path), err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return "", fmt.Errorf("artifact %s is not a bounded regular file", filepath.Base(path))
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("checksum %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
