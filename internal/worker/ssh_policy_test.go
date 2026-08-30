package worker

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestLoadSSHStartupConfigIsBoundedAndReadOnly(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	allowlistPath := filepath.Join(directory, "allowlist")
	knownHostsPath := filepath.Join(directory, "known_hosts")
	if err := os.WriteFile(allowlistPath, []byte("# deployment policy\n192.0.2.0/24\n192.0.2.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{"192.0.2.7"}, sshPublic) + "\n"
	if err := os.WriteFile(knownHostsPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadSSHStartupConfig(allowlistPath, knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Allowlist) != 1 || config.Allowlist[0] != netip.MustParsePrefix("192.0.2.0/24") || len(config.KnownHostsDigest) != 64 || config.HostKeyCallback == nil {
		t.Fatalf("config = %#v", config)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.7"), Port: 22}
	if err := config.HostKeyCallback("192.0.2.7:22", remote, sshPublic); err != nil {
		t.Fatalf("matching host key was rejected: %v", err)
	}
	otherPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSSH, err := ssh.NewPublicKey(otherPublic)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.HostKeyCallback("192.0.2.7:22", remote, otherSSH); err == nil {
		t.Fatal("mismatched host key was accepted")
	}
	if body, err := os.ReadFile(allowlistPath); err != nil || string(body) != "# deployment policy\n192.0.2.0/24\n192.0.2.0/24\n" {
		t.Fatalf("startup config was changed: %q, %v", body, err)
	}
}

func TestLoadSSHStartupConfigRejectsNoncanonicalOrEmptyPolicy(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "192.0.2.7/24\n", "2001:db8::/64\n"} {
		directory := t.TempDir()
		allowlistPath := filepath.Join(directory, "allowlist")
		knownHostsPath := filepath.Join(directory, "known_hosts")
		if err := os.WriteFile(allowlistPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(knownHostsPath, []byte("example.invalid ssh-ed25519 AAAA\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSSHStartupConfig(allowlistPath, knownHostsPath); err == nil {
			t.Fatalf("allowlist %q was accepted", body)
		}
	}
}
