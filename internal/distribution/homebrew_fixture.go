package distribution

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Nischoy-ai/topo/internal/release"
)

// WriteHomebrewFixture uses the production formula renderer with local,
// checksummed archive URLs, for pre-publication CI only. It creates no channel
// metadata and performs no signing or external publication.
func WriteHomebrewFixture(artifacts, out, version string) error {
	if _, err := release.ReadPolicy(artifacts, version); err != nil {
		return err
	}
	if !strings.Contains(version, "-") {
		return errors.New("Homebrew fixture requires a prerelease")
	}
	checksums, err := verifyArtifacts(artifacts)
	if err != nil {
		return err
	}
	if _, ok := checksums["release-metadata.json"]; !ok {
		return errors.New("fixture checksums omit release metadata")
	}
	filenameVersion := strings.TrimPrefix(version, "v")
	for _, platform := range []string{"darwin", "linux"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if _, ok := checksums["topo_"+filenameVersion+"_"+platform+"_"+arch+".tar.gz"]; !ok {
				return errors.New("fixture requires all four Linux/macOS archives")
			}
		}
	}
	abs, err := filepath.Abs(artifacts)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
		return errors.New("fixture output must not exist")
	}
	return writeHomebrewWithURLs(Options{OutputDir: out, Version: version, Channel: "beta"}, checksums, filenameVersion, func(name string) string {
		return (&url.URL{Scheme: "file", Path: filepath.Join(abs, name)}).String()
	})
}
