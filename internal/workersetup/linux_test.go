//go:build linux

package workersetup

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func fixture(t *testing.T) (*linuxBackend, settings, inputs) {
	t.Helper()
	root := t.TempDir()
	key, _ := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, 32)).Public())
	files := map[string][]byte{"token": []byte("synthetic-not-a-real-token\n"), "allow": []byte("192.0.2.10/32\n"), "hosts": []byte(knownhosts.Line([]string{"192.0.2.10"}, key) + "\n")}
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(root, n), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return &linuxBackend{dir: filepath.Join(root, "config"), ownerUID: os.Geteuid(), gid: os.Getegid(), sourceUID: os.Geteuid(), stagedCheck: func(context.Context, settings, string) error { return nil }}, settings{Instance: "https://example.service-now.com", Pool: "pool-a", Site: "site-a"}, inputs{Token: filepath.Join(root, "token"), Targets: filepath.Join(root, "allow"), Hosts: filepath.Join(root, "hosts")}
}
func TestRestrictiveInstallAndResume(t *testing.T) {
	b, s, i := fixture(t)
	if err := b.install(context.Background(), s, i); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"": 0750, "topo-worker.env": 0640, "worker-token": 0640, "targets.allow": 0640, "known_hosts": 0640, "setup.json": 0600} {
		st, err := os.Stat(filepath.Join(b.dir, name))
		if err != nil || st.Mode().Perm() != want {
			t.Fatalf("%s unsafe mode: %v", name, err)
		}
	}
	existing, err := b.existing(context.Background())
	if err != nil || *existing != s {
		t.Fatalf("resume: %v", err)
	}
	if err = b.install(context.Background(), s, i); err == nil {
		t.Fatal("existing deployment replaced")
	}
	if err = os.WriteFile(filepath.Join(b.dir, "targets.allow"), []byte("192.0.2.11/32\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = b.existing(context.Background()); err == nil {
		t.Fatal("changed policy allowed on resume")
	}
}
func TestExistingUnmanagedAndSymlinkPreserved(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		b, s, i := fixture(t)
		if symlink {
			if err := os.Symlink(filepath.Dir(b.dir), b.dir); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.Mkdir(b.dir, 0750); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := b.existing(context.Background()); err == nil {
			t.Fatal("unmanaged existing path accepted")
		}
		if err := b.install(context.Background(), s, i); err == nil {
			t.Fatal("existing path overwritten")
		}
		if _, err := os.Lstat(b.dir); err != nil {
			t.Fatal("existing path removed")
		}
	}
}
func TestInvalidFilesNeverPublishOrLeak(t *testing.T) {
	for _, kind := range []string{"token permissions", "token controls", "symlink", "fifo", "too large", "bad CIDR", "too broad", "unknown host", "revoked host"} {
		t.Run(kind, func(t *testing.T) {
			b, s, i := fixture(t)
			switch kind {
			case "token permissions":
				os.Chmod(i.Token, 0644)
			case "token controls":
				os.WriteFile(i.Token, []byte("SENSITIVE\vTOKEN"), 0600)
			case "symlink":
				os.Remove(i.Token)
				os.Symlink(i.Targets, i.Token)
			case "fifo":
				os.Remove(i.Token)
				if err := makeFIFO(i.Token); err != nil {
					t.Fatal(err)
				}
			case "too large":
				os.WriteFile(i.Token, []byte(strings.Repeat("SECRET", 12000)), 0600)
			case "bad CIDR":
				os.WriteFile(i.Targets, []byte("SECRET\n"), 0600)
			case "too broad":
				os.WriteFile(i.Targets, []byte("192.0.0.0/16\n"), 0600)
			case "unknown host":
				os.WriteFile(i.Targets, []byte("192.0.2.11/32\n"), 0600)
			case "revoked host":
				body, _ := os.ReadFile(i.Hosts)
				os.WriteFile(i.Hosts, append([]byte("@revoked "), body...), 0600)
			}
			err := b.install(context.Background(), s, i)
			if err == nil {
				t.Fatal("unsafe input accepted")
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("file bytes leaked")
			}
			if _, err = os.Stat(b.dir); !os.IsNotExist(err) {
				t.Fatal("failed install published configuration")
			}
			entries, _ := os.ReadDir(filepath.Dir(b.dir))
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".topo-worker-setup-") {
					t.Fatal("staging file leak")
				}
			}
		})
	}
}
func TestResumeRejectsPermissionChangeAndExtraFiles(t *testing.T) {
	for _, kind := range []string{"mode", "token symlink", "extra"} {
		t.Run(kind, func(t *testing.T) {
			b, s, i := fixture(t)
			if err := b.install(context.Background(), s, i); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mode":
				os.Chmod(filepath.Join(b.dir, "worker-token"), 0660)
			case "token symlink":
				os.Remove(filepath.Join(b.dir, "worker-token"))
				os.Symlink(i.Token, filepath.Join(b.dir, "worker-token"))
			case "extra":
				os.WriteFile(filepath.Join(b.dir, "other"), []byte("operator-owned"), 0600)
			}
			if _, err := b.existing(context.Background()); err == nil {
				t.Fatal("unsafe resume accepted")
			}
		})
	}
}
func TestPackagedUnitPinAndPolicyMatch(t *testing.T) {
	unit, err := os.ReadFile("../../packaging/systemd/topo-worker.package.service")
	if err != nil {
		t.Fatal(err)
	}
	if digest(unit) != unitSHA256 {
		t.Fatal("update reviewed setup unit pin with package changes")
	}
	b, s, _ := fixture(t)
	args := checkArgs(s, b.dir)
	if strings.Contains(strings.Join(args, " "), "synthetic-not-a-real-token") {
		t.Fatal("secret value in process arguments")
	}
	for _, flag := range []string{"-allow-ssh-linux", "-max-concurrency", "worker check"} {
		if !strings.Contains(string(unit), flag) {
			t.Fatal("setup no longer matches unit")
		}
	}
}

func TestPolicyCancellationAndOverlaps(t *testing.T) {
	b, _, i := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := settings{Instance: "https://example.service-now.com", Pool: "pool-a", Site: "site-a"}
	if err := b.install(ctx, s, i); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatal("cancelled validation accepted")
	}
	if _, err := os.Stat(b.dir); !os.IsNotExist(err) {
		t.Fatal("cancelled install published files")
	}
	// Overlapping singleton scopes must not count as separate addresses.
	if err := os.WriteFile(i.Targets, []byte("192.0.2.10/32\n192.0.2.10/32\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s = settings{Instance: "https://example.service-now.com", Pool: "pool-a", Site: "site-a"}
	if err := b.install(context.Background(), s, i); err != nil {
		t.Fatal(err)
	}
}

func TestTrustChecksDeduplicateAndCancel(t *testing.T) {
	calls := 0
	callback := func(string, net.Addr, ssh.PublicKey) error { calls++; return nil }
	prefixes := []netip.Prefix{netip.MustParsePrefix("192.0.2.10/31"), netip.MustParsePrefix("192.0.2.10/32"), netip.MustParsePrefix("192.0.2.10/31")}
	if err := validateTargets(context.Background(), prefixes, callback); err != nil || calls != 2 {
		t.Fatalf("overlap multiplied trust checks: %d, %v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	callback = func(string, net.Addr, ssh.PublicKey) error { calls++; cancel(); return nil }
	if err := validateTargets(ctx, prefixes, callback); err == nil || calls != 1 {
		t.Fatalf("did not cancel trust iteration: %d %v", calls, err)
	}
	calls = 0
	callback = func(string, net.Addr, ssh.PublicKey) error { calls++; return nil }
	if err := validateTargets(context.Background(), []netip.Prefix{netip.MustParsePrefix("192.0.0.0/16")}, callback); err == nil || calls != 1024 {
		t.Fatalf("unbounded expansion: %d %v", calls, err)
	}
}
func TestWrongOwnerReceiptRefused(t *testing.T) {
	b, s, i := fixture(t)
	if err := b.install(context.Background(), s, i); err != nil {
		t.Fatal(err)
	}
	b.ownerUID++
	if _, err := b.existing(context.Background()); err == nil {
		t.Fatal("wrong-owner files accepted")
	}
}

func TestPlatformRequiresPrivilegeBeforeMutation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root rejection requires an unprivileged test process")
	}
	b := &linuxBackend{dir: filepath.Join(t.TempDir(), "config")}
	if err := b.ready(context.Background()); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatal("non-root setup accepted")
	}
	if b.lock != nil {
		t.Fatal("unprivileged setup created lock")
	}
}
func TestSubprocessDeadlineAndEnvironment(t *testing.T) {
	t.Setenv("TOPO_SETUP_SYNTHETIC_SECRET", "never-inherit-this-value")
	if out, err := command(context.Background(), nil, "/usr/bin/printenv", "TOPO_SETUP_SYNTHETIC_SECRET"); err == nil || len(out) != 0 || strings.Contains(err.Error(), "never-inherit-this-value") {
		t.Fatal("subprocess inherited secret environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := command(ctx, nil, "/bin/sleep", "5"); err == nil {
		t.Fatal("subprocess ignored deadline")
	}
	var sink boundedOutput
	input := make([]byte, 32<<10)
	if n, err := sink.Write(input); n != len(input) || err != nil || !sink.overflow || len(sink.data) != 16<<10 {
		t.Fatal("unbounded subprocess capture")
	}
}

func TestInitialPreflightFailureLeavesSettingsReenterable(t *testing.T) {
	b, s, i := fixture(t)
	b.stagedCheck = func(_ context.Context, got settings, stage string) error {
		if got != s || stage == b.dir {
			t.Fatal("preflight did not use private staged settings")
		}
		st, err := os.Stat(stage)
		if err != nil || st.Mode().Perm() != 0750 {
			t.Fatal("service cannot traverse staged configuration")
		}
		token, err := os.ReadFile(filepath.Join(stage, "worker-token"))
		if err != nil || len(token) == 0 {
			t.Fatal("staged token missing")
		}
		return errors.New("SENSITIVE remote failure")
	}
	err := b.install(context.Background(), s, i)
	if err == nil || strings.Contains(err.Error(), "SENSITIVE") {
		t.Fatal("preflight failure accepted or leaked")
	}
	if _, err = os.Stat(b.dir); !os.IsNotExist(err) {
		t.Fatal("failed preflight permanently saved a typo")
	}
	existing, err := b.existing(context.Background())
	if err != nil || existing != nil {
		t.Fatal("cannot re-enter initial settings after failure")
	}
	s.Pool = "corrected-pool"
	b.stagedCheck = func(_ context.Context, got settings, _ string) error {
		if got.Pool != "corrected-pool" {
			t.Fatal("old typo reused")
		}
		return nil
	}
	if err = b.install(context.Background(), s, i); err != nil {
		t.Fatal(err)
	}
	saved, err := b.existing(context.Background())
	if err != nil || saved.Pool != "corrected-pool" {
		t.Fatal("corrected setting not published")
	}
}
