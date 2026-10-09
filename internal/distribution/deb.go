package distribution

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// debVersion reads the nFPM gzip control archive without extracting files or
// invoking maintainer scripts. Package identity and version must agree with
// the authenticated release; filenames are not Debian package versions.
func debVersion(path, version, arch string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != "!<arch>\n" {
		return "", errors.New("invalid Debian archive")
	}
	for member := 0; member < 16; member++ {
		header := make([]byte, 60)
		if _, err := io.ReadFull(f, header); err != nil {
			return "", errors.New("Debian control archive missing or truncated")
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 || size > maxArtifactSize || string(header[58:]) != "`\n" {
			return "", errors.New("invalid Debian member header")
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(header[:16])), "/")
		if name == "control.tar.gz" {
			if size > 1<<20 {
				return "", errors.New("Debian control archive exceeds limit")
			}
			gz, err := gzip.NewReader(io.LimitReader(f, size))
			if err != nil {
				return "", fmt.Errorf("read Debian control gzip: %w", err)
			}
			defer gz.Close()
			reader := tar.NewReader(io.LimitReader(gz, 1<<20))
			for {
				entry, err := reader.Next()
				if err != nil {
					return "", errors.New("Debian control file missing or truncated")
				}
				if entry.Name != "./control" && entry.Name != "control" {
					continue
				}
				if entry.Typeflag != tar.TypeReg || entry.Size < 1 || entry.Size > 64<<10 {
					return "", errors.New("invalid Debian control file")
				}
				fields := make(map[string]string)
				scanner := bufio.NewScanner(reader)
				for scanner.Scan() {
					key, value, ok := strings.Cut(scanner.Text(), ": ")
					if !ok || (key != "Package" && key != "Version" && key != "Architecture") {
						continue
					}
					if _, exists := fields[key]; exists {
						return "", errors.New("duplicate Debian control identity field")
					}
					fields[key] = value
				}
				if err := scanner.Err(); err != nil {
					return "", fmt.Errorf("read Debian control: %w", err)
				}
				expected := strings.Replace(version, "-", "~", 1) + "-1"
				if fields["Package"] != "topo" || fields["Architecture"] != arch || fields["Version"] != expected {
					return "", errors.New("Debian control identity does not match release version and architecture")
				}
				return fields["Version"], nil
			}
		}
		if _, err := io.CopyN(io.Discard, f, size+size%2); err != nil {
			return "", errors.New("truncated Debian archive member")
		}
	}
	return "", errors.New("Debian archive has too many members")
}
