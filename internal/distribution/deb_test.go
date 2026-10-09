package distribution

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureDeb(t *testing.T, name, version, arch string) []byte {
	t.Helper()
	control := []byte(fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\n", name, version, arch))
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tr := tar.NewWriter(gz)
	if err := tr.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Write(control); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	archive.WriteString("!<arch>\n")
	for _, item := range []struct {
		name     string
		contents []byte
	}{
		{"debian-binary", []byte("2.0\n")}, {"control.tar.gz", compressed.Bytes()},
	} {
		fmt.Fprintf(&archive, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", item.name+"/", 0, 0, 0, "100644", len(item.contents))
		archive.Write(item.contents)
		if len(item.contents)%2 != 0 {
			archive.WriteByte('\n')
		}
	}
	return archive.Bytes()
}

func TestDebianControlIdentity(t *testing.T) {
	for _, test := range []struct {
		name, version, arch string
		valid               bool
	}{
		{"topo", "0.4.6~beta.1-1", "amd64", true},
		{"topo", "0.4.6~beta.1-1", "arm64", true},
		{"other", "0.4.6~beta.1-1", "amd64", false},
		{"topo", "0.4.6-beta.1", "amd64", false},
		{"topo", "0.4.7~beta.1-1", "amd64", false},
		{"topo", "0.4.6~beta.1-1", "s390x", false},
		{"topo", "0.4.6~beta.1-1\nVersion: 0.4.6", "amd64", false},
	} {
		path := filepath.Join(t.TempDir(), "package.deb")
		if err := os.WriteFile(path, fixtureDeb(t, test.name, test.version, test.arch), 0o600); err != nil {
			t.Fatal(err)
		}
		arch := test.arch
		if arch == "s390x" {
			arch = "amd64"
		}
		got, err := debVersion(path, "0.4.6-beta.1", arch)
		if (err == nil) != test.valid || (test.valid && got != test.version) {
			t.Fatalf("%+v: got %q, %v", test, got, err)
		}
	}
	for _, contents := range [][]byte{[]byte("not a deb"), []byte("!<arch>\n"), append([]byte("!<arch>\n"), bytes.Repeat([]byte("x"), 60)...)} {
		path := filepath.Join(t.TempDir(), "broken.deb")
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := debVersion(path, "0.4.6-beta.1", "amd64"); err == nil {
			t.Fatal("accepted malformed Debian archive")
		}
	}
}

func TestAPTAdvertisesNativeVersionAndRenewsWithoutChangingPackages(t *testing.T) {
	for _, release := range []struct{ tag, channel, version string }{
		{"v0.4.6-beta.1", "beta", "0.4.6~beta.1-1"},
		{"v0.4.6", "stable", "0.4.6-1"},
	} {
		artifacts := filepath.Join(t.TempDir(), "release")
		writeFixture(t, artifacts, release.tag)
		var previous map[string][]byte
		for _, days := range []int{0, 30} {
			out := filepath.Join(t.TempDir(), "out")
			options := validOptions(artifacts, out, release.tag, release.channel)
			options.PublishedAt = options.PublishedAt.Add(time.Duration(days) * 24 * time.Hour)
			if err := Build(options); err != nil {
				t.Fatal(err)
			}
			files := readTree(t, out)
			root := "apt/dists/" + release.channel + "/"
			for _, arch := range []string{"amd64", "arm64"} {
				index := string(files[root+"main/binary-"+arch+"/Packages"])
				if !strings.Contains(index, "\nVersion: "+release.version+"\n") {
					t.Fatalf("wrong native version: %s", index)
				}
			}
			want := "Valid-Until: " + options.PublishedAt.Add(90*24*time.Hour).Format(time.RFC1123) + "\n"
			if !strings.Contains(string(files[root+"Release"]), want) {
				t.Fatalf("missing bounded expiry: %s", want)
			}
			if previous != nil {
				for name, data := range files {
					if strings.Contains(name, "/pool/") || strings.Contains(name, "Packages") || strings.Contains(name, "by-hash") {
						if !bytes.Equal(data, previous[name]) {
							t.Fatalf("renewal changed %s", name)
						}
					}
				}
				if bytes.Equal(files[root+"Release"], previous[root+"Release"]) {
					t.Fatal("renewal did not advance expiry")
				}
			}
			previous = files
		}
	}
}

func TestDebianBetaToStableOrdering(t *testing.T) {
	dpkg, err := exec.LookPath("dpkg")
	if err != nil {
		t.Skip("dpkg comparisons are mandatory on Linux package CI")
	}
	for _, pair := range [][2]string{{"0.4.6~beta.1-1", "0.4.6~beta.2-1"}, {"0.4.6~beta.2-1", "0.4.6-1"}, {"0.4.6~beta.1-1", "0.4.6"}} {
		if err := exec.Command(dpkg, "--compare-versions", pair[0], "lt", pair[1]).Run(); err != nil {
			t.Fatalf("%s should upgrade to %s: %v", pair[0], pair[1], err)
		}
	}
}
