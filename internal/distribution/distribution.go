// Package distribution promotes verified Topo release artifacts into native
// package-manager layouts. It never compiles or repackages application code.
package distribution

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxArtifactSize = 512 << 20

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// Options controls deterministic package-manager promotion input generation.
type Options struct {
	ArtifactDir       string
	OutputDir         string
	Version           string
	Channel           string
	ReleaseBaseURL    string
	RepositoryBaseURL string
	PublishedAt       time.Time
}

type releaseMetadata struct {
	SchemaVersion int    `json:"schema_version"`
	Project       string `json:"project"`
	Repository    string `json:"repository"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
}

type promotionMetadata struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Commit        string            `json:"commit"`
	Channel       string            `json:"channel"`
	PublishedAt   string            `json:"published_at"`
	Source        string            `json:"source"`
	Artifacts     map[string]string `json:"artifacts"`
}

type checksumEntry struct {
	Digest string
	Size   int64
}

// Build creates unsigned APT/RPM repository inputs, a Homebrew formula,
// WinGet manifests, and an OCI Helm input from verified release artifacts.
// Repository-native signatures are added by a protected promotion job.
func Build(options Options) (err error) {
	if err := validateOptions(&options); err != nil {
		return err
	}
	if _, err := os.Lstat(options.OutputDir); err == nil {
		return fmt.Errorf("output already exists: %s", options.OutputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create distribution output: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(options.OutputDir)
		}
	}()

	checksums, err := verifyArtifacts(options.ArtifactDir)
	if err != nil {
		return err
	}
	if _, ok := checksums["release-metadata.json"]; !ok {
		return errors.New("release checksum manifest omits release-metadata.json")
	}
	metadataBytes, err := os.ReadFile(filepath.Join(options.ArtifactDir, "release-metadata.json"))
	if err != nil {
		return fmt.Errorf("read release metadata: %w", err)
	}
	var release releaseMetadata
	if err := json.Unmarshal(metadataBytes, &release); err != nil {
		return fmt.Errorf("parse release metadata: %w", err)
	}
	if release.SchemaVersion != 1 || release.Project != "Nischoy Topo" ||
		release.Repository != "https://github.com/Nischoy-ai/topo" ||
		release.Version != options.Version || !commitPattern.MatchString(release.Commit) {
		return fmt.Errorf("release metadata does not describe %s", options.Version)
	}

	filenameVersion := strings.TrimPrefix(options.Version, "v")
	required := []string{
		"topo_" + filenameVersion + "_amd64.deb",
		"topo_" + filenameVersion + "_arm64.deb",
		"topo-" + filenameVersion + "-1.x86_64.rpm",
		"topo-" + filenameVersion + "-1.aarch64.rpm",
		"topo_" + filenameVersion + "_darwin_amd64.tar.gz",
		"topo_" + filenameVersion + "_darwin_arm64.tar.gz",
		"topo_" + filenameVersion + "_linux_amd64.tar.gz",
		"topo_" + filenameVersion + "_linux_arm64.tar.gz",
		"topo_" + filenameVersion + "_windows_amd64.msi",
		"topo_" + filenameVersion + "_windows_arm64.msi",
		"topo-" + filenameVersion + ".tgz",
	}
	selected := make(map[string]string, len(required))
	for _, name := range required {
		entry, ok := checksums[name]
		if !ok {
			return fmt.Errorf("release checksum manifest omits required artifact %s", name)
		}
		selected[name] = entry.Digest
	}

	if err := writeAPT(options, checksums, filenameVersion); err != nil {
		return err
	}
	if err := writeRPM(options, filenameVersion); err != nil {
		return err
	}
	if err := writeHomebrew(options, checksums, filenameVersion); err != nil {
		return err
	}
	if err := writeWinGet(options, checksums, filenameVersion); err != nil {
		return err
	}
	chart := "topo-" + filenameVersion + ".tgz"
	if err := copyArtifact(options, chart, filepath.Join("helm", chart)); err != nil {
		return err
	}

	promotion := promotionMetadata{
		SchemaVersion: 1,
		Version:       options.Version,
		Commit:        release.Commit,
		Channel:       options.Channel,
		PublishedAt:   options.PublishedAt.Format(time.RFC3339),
		Source:        strings.TrimRight(options.ReleaseBaseURL, "/") + "/releases/tag/" + options.Version,
		Artifacts:     selected,
	}
	contents, err := json.MarshalIndent(promotion, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(filepath.Join(options.OutputDir, "promotion-metadata.json"), contents, 0o644); err != nil {
		return err
	}
	complete = true
	return nil
}

func validateOptions(options *Options) error {
	if !versionPattern.MatchString(options.Version) {
		return fmt.Errorf("version %q must be a semantic release tag", options.Version)
	}
	if options.Channel != "stable" && options.Channel != "beta" {
		return fmt.Errorf("channel must be stable or beta")
	}
	prerelease := strings.Contains(strings.TrimPrefix(options.Version, "v"), "-")
	if options.Channel == "stable" && prerelease {
		return errors.New("stable promotion rejects prerelease versions")
	}
	if options.Channel == "beta" && !prerelease {
		return errors.New("beta promotion requires a prerelease version")
	}
	if options.PublishedAt.IsZero() || !options.PublishedAt.Equal(options.PublishedAt.UTC().Truncate(time.Second)) {
		return errors.New("published-at must be a whole-second UTC timestamp")
	}
	for label, raw := range map[string]string{
		"release base URL":    options.ReleaseBaseURL,
		"repository base URL": options.RepositoryBaseURL,
	} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("%s must be an absolute HTTPS URL", label)
		}
	}
	for label, value := range map[string]*string{"artifact directory": &options.ArtifactDir, "output directory": &options.OutputDir} {
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

func verifyArtifacts(dir string) (map[string]checksumEntry, error) {
	file, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, fmt.Errorf("open release checksum manifest: %w", err)
	}
	defer file.Close()
	entries := make(map[string]checksumEntry)
	scanner := bufio.NewScanner(io.LimitReader(file, 1<<20))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "  ", 2)
		if len(parts) != 2 || !safeName(parts[1]) || len(parts[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return nil, fmt.Errorf("invalid checksum for %s", parts[1])
		}
		if _, duplicate := entries[parts[1]]; duplicate {
			return nil, fmt.Errorf("duplicate checksum for %s", parts[1])
		}
		path := filepath.Join(dir, parts[1])
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", parts[1], err)
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxArtifactSize {
			return nil, fmt.Errorf("artifact %s is not a bounded regular file", parts[1])
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return nil, err
		}
		if digest != parts[0] {
			return nil, fmt.Errorf("checksum mismatch for %s", parts[1])
		}
		entries[parts[1]] = checksumEntry{Digest: digest, Size: info.Size()}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func writeAPT(options Options, checksums map[string]checksumEntry, version string) error {
	var indexFiles []string
	for _, arch := range []string{"amd64", "arm64"} {
		name := fmt.Sprintf("topo_%s_%s.deb", version, arch)
		poolPath := filepath.ToSlash(filepath.Join("pool", "main", "t", "topo", name))
		if err := copyArtifact(options, name, filepath.Join("apt", filepath.FromSlash(poolPath))); err != nil {
			return err
		}
		entry := checksums[name]
		packages := fmt.Sprintf("Package: topo\nVersion: %s\nArchitecture: %s\nMaintainer: Nischoy <security@nischoy.com>\nFilename: %s\nSize: %d\nSHA256: %s\nSection: admin\nPriority: optional\nHomepage: https://github.com/Nischoy-ai/topo\nDescription: Destination-neutral infrastructure discovery data plane\n\n", version, arch, poolPath, entry.Size, entry.Digest)
		relative := filepath.ToSlash(filepath.Join("main", "binary-"+arch, "Packages"))
		path := filepath.Join(options.OutputDir, "apt", "dists", options.Channel, filepath.FromSlash(relative))
		if err := writeFile(path, []byte(packages)); err != nil {
			return err
		}
		gzPath := path + ".gz"
		if err := writeGzip(gzPath, []byte(packages)); err != nil {
			return err
		}
		for _, candidate := range []string{path, gzPath} {
			digest, err := fileSHA256(candidate)
			if err != nil {
				return err
			}
			byHash := filepath.Join(filepath.Dir(candidate), "by-hash", "SHA256", digest)
			if err := copyFile(candidate, byHash); err != nil {
				return err
			}
			indexFiles = append(indexFiles, strings.TrimPrefix(filepath.ToSlash(candidate), filepath.ToSlash(filepath.Join(options.OutputDir, "apt", "dists", options.Channel))+"/"))
		}
	}
	sort.Strings(indexFiles)
	var release strings.Builder
	fmt.Fprintf(&release, "Origin: Nischoy\nLabel: Nischoy Topo\nSuite: %s\nCodename: %s\n", options.Channel, options.Channel)
	fmt.Fprintf(&release, "Date: %s\nValid-Until: %s\n", options.PublishedAt.Format(time.RFC1123), options.PublishedAt.Add(30*24*time.Hour).Format(time.RFC1123))
	release.WriteString("Architectures: amd64 arm64\nComponents: main\nDescription: Nischoy Topo packages\nAcquire-By-Hash: yes\nSHA256:\n")
	root := filepath.Join(options.OutputDir, "apt", "dists", options.Channel)
	for _, name := range indexFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&release, " %s %d %s\n", digest, info.Size(), name)
	}
	if err := writeFile(filepath.Join(root, "Release"), []byte(release.String())); err != nil {
		return err
	}
	sources := fmt.Sprintf("Types: deb\nURIs: %s/apt\nSuites: %s\nComponents: main\nArchitectures: amd64 arm64\nSigned-By: /etc/apt/keyrings/nischoy-topo.gpg\n", strings.TrimRight(options.RepositoryBaseURL, "/"), options.Channel)
	return writeFile(filepath.Join(options.OutputDir, "apt", "nischoy-topo-"+options.Channel+".sources"), []byte(sources))
}

func writeRPM(options Options, version string) error {
	for _, item := range []struct{ arch, name string }{
		{"x86_64", "topo-" + version + "-1.x86_64.rpm"},
		{"aarch64", "topo-" + version + "-1.aarch64.rpm"},
	} {
		if err := copyArtifact(options, item.name, filepath.Join("rpm", options.Channel, item.arch, "Packages", "t", item.name)); err != nil {
			return err
		}
	}
	repo := fmt.Sprintf("[nischoy-topo-%s]\nname=Nischoy Topo (%s)\nbaseurl=%s/rpm/%s/$basearch\nenabled=1\ngpgcheck=1\nrepo_gpgcheck=1\ngpgkey=%s/keys/nischoy-topo-archive.gpg\n", options.Channel, options.Channel, strings.TrimRight(options.RepositoryBaseURL, "/"), options.Channel, strings.TrimRight(options.RepositoryBaseURL, "/"))
	return writeFile(filepath.Join(options.OutputDir, "rpm", "nischoy-topo-"+options.Channel+".repo"), []byte(repo))
}

func writeHomebrew(options Options, checksums map[string]checksumEntry, version string) error {
	assetURL := func(name string) string {
		return strings.TrimRight(options.ReleaseBaseURL, "/") + "/releases/download/" + options.Version + "/" + name
	}
	name := func(platform, arch string) string {
		return fmt.Sprintf("topo_%s_%s_%s.tar.gz", version, platform, arch)
	}
	formulaName := "topo"
	className := "Topo"
	conflict := "  conflicts_with \"topo-beta\", because: \"both install the topo executable\"\n"
	if options.Channel == "beta" {
		formulaName = "topo-beta"
		className = "TopoBeta"
		conflict = "  conflicts_with \"topo\", because: \"both install the topo executable\"\n"
	}
	formula := fmt.Sprintf(`class %s < Formula
  desc "Destination-neutral infrastructure discovery data plane"
  homepage "https://github.com/Nischoy-ai/topo"
  version %q
  license "Apache-2.0"
%s

  on_macos do
    on_intel do
      url %q
      sha256 %q
    end
    on_arm do
      url %q
      sha256 %q
    end
  end

  on_linux do
    on_intel do
      url %q
      sha256 %q
    end
    on_arm do
      url %q
      sha256 %q
    end
  end

  def install
    bin.install "topo"
    doc.install "LICENSE", "README.md"
  end

  test do
    assert_equal %q, shell_output("#{bin}/topo version").strip
  end
end
`, className, version, conflict,
		assetURL(name("darwin", "amd64")), checksums[name("darwin", "amd64")].Digest,
		assetURL(name("darwin", "arm64")), checksums[name("darwin", "arm64")].Digest,
		assetURL(name("linux", "amd64")), checksums[name("linux", "amd64")].Digest,
		assetURL(name("linux", "arm64")), checksums[name("linux", "arm64")].Digest,
		options.Version,
	)
	return writeFile(filepath.Join(options.OutputDir, "homebrew", "Formula", formulaName+".rb"), []byte(formula))
}

func writeWinGet(options Options, checksums map[string]checksumEntry, version string) error {
	root := filepath.Join(options.OutputDir, "winget", "manifests", "n", "Nischoy", "Topo", version)
	assetURL := func(arch string) string {
		return strings.TrimRight(options.ReleaseBaseURL, "/") + "/releases/download/" + options.Version + "/topo_" + version + "_windows_" + arch + ".msi"
	}
	installer := fmt.Sprintf(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.1.10.0.schema.json
PackageIdentifier: Nischoy.Topo
PackageVersion: %s
InstallerType: msi
Scope: machine
UpgradeBehavior: install
Commands:
  - topo
Installers:
  - Architecture: x64
    InstallerUrl: %s
    InstallerSha256: %s
    ProductCode: %s
  - Architecture: arm64
    InstallerUrl: %s
    InstallerSha256: %s
    ProductCode: %s
ManifestType: installer
ManifestVersion: 1.10.0
`, version, assetURL("amd64"), strings.ToUpper(checksums["topo_"+version+"_windows_amd64.msi"].Digest), productCode(options.Version, "amd64"), assetURL("arm64"), strings.ToUpper(checksums["topo_"+version+"_windows_arm64.msi"].Digest), productCode(options.Version, "arm64"))
	locale := fmt.Sprintf(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.1.10.0.schema.json
PackageIdentifier: Nischoy.Topo
PackageVersion: %s
PackageLocale: en-US
Publisher: Nischoy
PublisherUrl: https://github.com/Nischoy-ai
PublisherSupportUrl: https://github.com/Nischoy-ai/topo/issues
PackageName: Nischoy Topo
PackageUrl: https://github.com/Nischoy-ai/topo
License: Apache-2.0
LicenseUrl: https://github.com/Nischoy-ai/topo/blob/%s/LICENSE
ShortDescription: Destination-neutral infrastructure discovery data plane
Tags:
  - cmdb
  - discovery
  - infrastructure
ManifestType: defaultLocale
ManifestVersion: 1.10.0
`, version, options.Version)
	versionManifest := fmt.Sprintf(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.10.0.schema.json
PackageIdentifier: Nischoy.Topo
PackageVersion: %s
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.10.0
`, version)
	for name, contents := range map[string]string{
		"Nischoy.Topo.installer.yaml":    installer,
		"Nischoy.Topo.locale.en-US.yaml": locale,
		"Nischoy.Topo.yaml":              versionManifest,
	} {
		if err := writeFile(filepath.Join(root, name), []byte(contents)); err != nil {
			return err
		}
	}
	return nil
}

func productCode(version, arch string) string {
	namespace, _ := hex.DecodeString("cd5f19c6924f5f8b9de4c1b5e37346fe")
	digest := sha1.Sum(append(namespace, []byte("product/"+version+"/"+arch)...))
	b := append([]byte(nil), digest[:16]...)
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("{%x-%x-%x-%x-%x}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}

func copyArtifact(options Options, name, relative string) error {
	return copyFile(filepath.Join(options.ArtifactDir, name), filepath.Join(options.OutputDir, relative))
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxArtifactSize {
		return fmt.Errorf("input %s is not a bounded regular file", filepath.Base(source))
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, io.LimitReader(input, maxArtifactSize+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0o644)
}

func writeGzip(path string, contents []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	writer, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	writer.Header.ModTime = time.Time{}
	writer.Header.OS = 255
	if _, err := writer.Write(contents); err != nil {
		return err
	}
	return writer.Close()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, maxArtifactSize+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func safeName(name string) bool {
	return name != "" && filepath.Base(name) == name && name != "." && name != ".." && !strings.ContainsAny(name, "\\/\x00")
}
