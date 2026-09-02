package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Nischoy-ai/topo/internal/store/sqlite"
	"github.com/Nischoy-ai/topo/pkg/credentialref"
)

func TestResolveCredential(t *testing.T) {
	t.Setenv("TOPO_TEST_DEFAULT", "default-secret")
	t.Setenv("TOPO_TEST_LEGACY", "legacy-secret")
	t.Setenv("TOPO_TEST_EXPLICIT", "explicit-secret")
	tests := []struct {
		name              string
		reference         string
		legacyEnvironment string
		want              string
	}{
		{name: "default", want: "default-secret"},
		{name: "legacy", legacyEnvironment: "TOPO_TEST_LEGACY", want: "legacy-secret"},
		{name: "reference", reference: "env:TOPO_TEST_EXPLICIT", want: "explicit-secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := resolveCredential(test.reference, test.legacyEnvironment, "TOPO_TEST_DEFAULT", false)
			if err != nil {
				t.Fatal(err)
			}
			if string(value) != test.want {
				t.Fatalf("value = %q, want %q", value, test.want)
			}
		})
	}
}

func TestResolveCredentialOptionalDefault(t *testing.T) {
	const name = "TOPO_TEST_OPTIONAL_MISSING"
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	value, err := resolveCredential("", "", name, true)
	if err != nil || value != nil {
		t.Fatalf("value = %q, error = %v", value, err)
	}
	_, err = resolveCredential("env:"+name, "", name, true)
	if !errors.Is(err, credentialref.ErrUnavailable) {
		t.Fatalf("explicit missing reference error = %v", err)
	}
}

func TestResolveCredentialRejectsConflictingFlags(t *testing.T) {
	_, err := resolveCredential("env:ONE", "TWO", "DEFAULT", false)
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v", err)
	}
}

func TestMIDRunRejectsPositionalArguments(t *testing.T) {
	err := midRun([]string{"unexpected"})
	if err == nil || !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Fatalf("error = %v", err)
	}
}

func TestWorkerRunRequiresExplicitReadOnlyPolicy(t *testing.T) {
	t.Setenv("SERVICENOW_INSTANCE_URL", "")
	t.Setenv("TOPO_SERVICENOW_WORKER_TOKEN", "")
	tests := []struct {
		args []string
		want string
	}{
		{args: nil, want: "-servicenow-instance"},
		{args: []string{"-servicenow-instance", "https://example.service-now.com"}, want: "-worker-pool"},
		{args: []string{"-servicenow-instance", "https://example.service-now.com", "-worker-pool", "pool-a"}, want: "-site"},
		{args: []string{"-servicenow-instance", "https://example.service-now.com", "-worker-pool", "pool-a", "-site", "site-a"}, want: "explicitly allow at least one"},
		{args: []string{"-servicenow-instance", "https://example.service-now.com", "-worker-pool", "pool-a", "-site", "site-a", "-ssh-target-allowlist", "/tmp/targets"}, want: "require -allow-ssh-linux"},
		{args: []string{"-servicenow-instance", "https://example.service-now.com", "-worker-pool", "pool-a", "-site", "site-a", "-allow-ssh-linux"}, want: "path must be absolute"},
		{args: []string{"-state-dir", "/tmp/not-allowed"}, want: "flag provided but not defined"},
		{args: []string{"unexpected"}, want: "does not accept positional arguments"},
	}
	for _, test := range tests {
		err := workerRun(test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("workerRun(%q) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestWorkerCommandExposesOnlyRun(t *testing.T) {
	for _, args := range [][]string{nil, {"install"}, {"run", "unexpected"}} {
		err := runWorker(args)
		if err == nil {
			t.Fatalf("runWorker(%q) succeeded", args)
		}
	}
}

func TestResolvePrivateKeyReference(t *testing.T) {
	path := filepath.Join("testdata", "key")
	reference, err := resolvePrivateKeyReference("", path)
	if err != nil {
		t.Fatal(err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if reference != "file:"+absolutePath {
		t.Fatalf("reference = %q", reference)
	}
	if _, err := resolvePrivateKeyReference("env:KEY", path); err == nil {
		t.Fatal("conflicting private key flags were accepted")
	}
}

func TestParseSourcePrecedence(t *testing.T) {
	plugins, err := parseSourcePrecedence("vmware, ssh-linux,aws-organizations")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plugins, ",") != "vmware,ssh-linux,aws-organizations" {
		t.Fatalf("plugins = %#v", plugins)
	}
	if plugins, err := parseSourcePrecedence("  "); err != nil || plugins != nil {
		t.Fatalf("empty precedence = %#v, %v", plugins, err)
	}
	if _, err := parseSourcePrecedence("vmware,vmware"); err == nil {
		t.Fatal("duplicate source precedence accepted")
	}
}

func TestDecodeSpoolKey(t *testing.T) {
	value, err := decodeSpoolKey([]byte("  " + strings.Repeat("ab", 32) + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 32 {
		t.Fatalf("len(value) = %d, want 32", len(value))
	}

	tests := []string{
		"too-short",
		strings.Repeat("ab", 31), // 62 hex chars, 31 bytes
		strings.Repeat("zz", 32), // invalid hex
		strings.Repeat("ab", 33), // 66 hex chars, 33 bytes
	}
	for _, key := range tests {
		if _, err := decodeSpoolKey([]byte(key)); err == nil {
			t.Fatalf("decodeSpoolKey(%q) accepted an invalid key", key)
		}
	}
}

func TestAgentInstallRequiresControllerURLAndSpoolDir(t *testing.T) {
	if err := agentInstall([]string{"-spool-dir", "/var/lib/topo-agent/spool"}); err == nil || !strings.Contains(err.Error(), "-controller-url") {
		t.Fatalf("error = %v", err)
	}
	if err := agentInstall([]string{"-controller-url", "https://topo-hub.internal:8443"}); err == nil || !strings.Contains(err.Error(), "-spool-dir") {
		t.Fatalf("error = %v", err)
	}
}

func TestAgentInstallAndUninstallAreWindowsOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this assertion only holds for the non-Windows stub")
	}
	err := agentInstall([]string{"-controller-url", "https://topo-hub.internal:8443", "-spool-dir", "/var/lib/topo-agent/spool"})
	if err == nil || !strings.Contains(err.Error(), "Windows service management is only supported on windows") {
		t.Fatalf("error = %v, want a Windows-only error on this platform", err)
	}
	if err := agentUninstall(nil); err == nil || !strings.Contains(err.Error(), "Windows service management is only supported on windows") {
		t.Fatalf("error = %v, want a Windows-only error on this platform", err)
	}
}

func TestStorageBackupRestoreCommands(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	backup := filepath.Join(directory, "backup.db")
	restored := filepath.Join(directory, "restored.db")
	db, err := sqlite.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runStorage([]string{"backup", "-db-dsn", source, "-out", backup}); err != nil {
		t.Fatal(err)
	}
	if err := runStorage([]string{"restore", "-from", backup, "-db-dsn", restored}); err != nil {
		t.Fatal(err)
	}
	opened, err := sqlite.Open(restored)
	if err != nil {
		t.Fatalf("open database restored through CLI: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStorageCommandsRequirePaths(t *testing.T) {
	for _, args := range [][]string{
		{"backup"},
		{"backup", "-db-dsn", "source.db"},
		{"restore"},
		{"restore", "-from", "backup.db"},
	} {
		if err := runStorage(args); err == nil {
			t.Fatalf("runStorage(%q) accepted missing paths", args)
		}
	}
}

func TestStorageBackupDoesNotCreateMissingSource(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.db")
	if err := runStorage([]string{"backup", "-db-dsn", missing, "-out", missing + ".backup"}); err == nil {
		t.Fatal("storage backup accepted a missing source database")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("storage backup created its missing source: %v", err)
	}
}
