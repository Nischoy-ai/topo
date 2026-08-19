package enrollment

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"path/filepath"
	"testing"
	"time"
)

func generateTestCSR(t *testing.T, commonName string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: commonName}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

func TestLoadOrCreateCAGeneratesThenPersists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	first, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.CACertPEM()) != string(second.CACertPEM()) {
		t.Fatal("reloaded CA has a different certificate than the persisted one")
	}
}

func TestLoadOrCreateCARejectsRelativeDir(t *testing.T) {
	if _, err := LoadOrCreateCA("relative/dir"); err == nil {
		t.Fatal("expected a relative CA directory to be rejected")
	}
}

func TestSignIssuesVerifiableCertificate(t *testing.T) {
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	csrDER, _ := generateTestCSR(t, "collector-1")
	csr, err := ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := ca.Sign(csr, "collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.CACertPEM()) {
		t.Fatal("failed to load CA cert into pool")
	}
	block, rest := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("expected a PEM block")
	}
	if len(rest) != 0 {
		t.Fatal("unexpected trailing data after certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "collector-1" {
		t.Fatalf("common name = %q", cert.Subject.CommonName)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("issued certificate does not verify against the CA: %v", err)
	}
}

func TestParseCSRRejectsTamperedSignature(t *testing.T) {
	csrDER, _ := generateTestCSR(t, "collector-1")
	tampered := append([]byte(nil), csrDER...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := ParseCSR(tampered); err == nil {
		t.Fatal("expected a tampered CSR to be rejected")
	}
}

func TestSignRejectsNonPositiveTTL(t *testing.T) {
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	csrDER, _ := generateTestCSR(t, "collector-1")
	csr, err := ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Sign(csr, "collector-1", 0); err == nil {
		t.Fatal("expected a non-positive ttl to be rejected")
	}
}

func TestIssueServerCertificateVerifiesForServerAuth(t *testing.T) {
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := ca.IssueServerCertificate([]string{"localhost", "127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("issued server certificate and key do not form a valid pair: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.CACertPEM()) {
		t.Fatal("failed to load CA cert into pool")
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("expected a PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("issued server certificate does not verify for localhost: %v", err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err == nil {
		t.Fatal("server certificate should not verify for client authentication")
	}
}

func TestIssueServerCertificateRequiresSANs(t *testing.T) {
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ca.IssueServerCertificate(nil, time.Hour); err == nil {
		t.Fatal("expected an empty SAN list to be rejected")
	}
}

func TestIssueServerCertificateRejectsNonPositiveTTL(t *testing.T) {
	ca, err := generateCA()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ca.IssueServerCertificate([]string{"localhost"}, 0); err == nil {
		t.Fatal("expected a non-positive ttl to be rejected")
	}
}

func TestValidCollectorID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"host-1.example.test", true},
		{"", false},
		{" host-1", false},
		{"host\x001", false},
		{string(make([]byte, maxCollectorIDBytes+1)), false},
	}
	for _, test := range tests {
		if got := ValidCollectorID(test.id); got != test.want {
			t.Errorf("ValidCollectorID(%q) = %v, want %v", test.id, got, test.want)
		}
	}
}
