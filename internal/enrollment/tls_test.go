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
