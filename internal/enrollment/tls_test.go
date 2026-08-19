package enrollment

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func writeEnrollmentFiles(t *testing.T, dir string) {
	t.Helper()
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	csrDER, key := generateTestCSR(t, "collector-1")
	csr, err := ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := ca.Sign(csr, "collector-1", caValidity)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, ClientCertFile), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ClientKeyFile), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, CACertFile), ca.CACertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadClientTLSConfig(t *testing.T) {
	dir := t.TempDir()
	writeEnrollmentFiles(t, dir)

	cfg, err := LoadClientTLSConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("certificates = %d, want 1", len(cfg.Certificates))
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated")
	}
}

func TestLoadClientTLSConfigRejectsRelativeDir(t *testing.T) {
	if _, err := LoadClientTLSConfig("relative/dir"); err == nil {
		t.Fatal("expected a relative directory to be rejected")
	}
}

func TestLoadClientTLSConfigMissingFiles(t *testing.T) {
	if _, err := LoadClientTLSConfig(t.TempDir()); err == nil {
		t.Fatal("expected missing certificate files to be rejected")
	}
}

func TestWriteResultThenLoadRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cert-dir")
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	csrDER, _ := generateTestCSR(t, "collector-1")
	csr, err := ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := ca.Sign(csr, "collector-1", caValidity)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{
		CertificatePEM:   certPEM,
		PrivateKeyPEM:    []byte("not a real key, only round-tripping matters here"),
		CACertificatePEM: ca.CACertPEM(),
	}

	if err := WriteResult(dir, result); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ClientCertFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(result.CertificatePEM) {
		t.Fatal("written client certificate does not match what was passed to WriteResult")
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("cert directory permissions = %v, want 0700 (err=%v)", info, err)
	}
}

func TestWriteResultRejectsRelativeDir(t *testing.T) {
	if err := WriteResult("relative/dir", Result{}); err == nil {
		t.Fatal("expected a relative directory to be rejected")
	}
}

func TestCurrentCollectorID(t *testing.T) {
	dir := t.TempDir()
	writeEnrollmentFiles(t, dir)

	id, err := CurrentCollectorID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id != "collector-1" {
		t.Fatalf("collector ID = %q, want collector-1", id)
	}
}

func TestCurrentCollectorIDRejectsRelativeDir(t *testing.T) {
	if _, err := CurrentCollectorID("relative/dir"); err == nil {
		t.Fatal("expected a relative directory to be rejected")
	}
}

func TestCurrentCollectorIDMissingFile(t *testing.T) {
	if _, err := CurrentCollectorID(t.TempDir()); err == nil {
		t.Fatal("expected a missing certificate file to be rejected")
	}
}
