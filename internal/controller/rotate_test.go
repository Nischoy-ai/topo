package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
)

func parsePEMCertificate(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certificate PEM block not found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func parsePEMECKey(t *testing.T, keyPEM []byte) *ecdsa.PrivateKey {
	t.Helper()
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("private key PEM block not found")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestRotateClientEndToEnd(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	oldCert, err := x509.ParseCertificate(clientCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{clientCert}, RootCAs: caPool}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := enrollment.Rotate(ctx, server.URL, tlsConfig, "collector-1")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	newCert := parsePEMCertificate(t, result.CertificatePEM)
	if newCert.Subject.CommonName != "collector-1" {
		t.Fatalf("rotated certificate common name = %q, want collector-1", newCert.Subject.CommonName)
	}
	if newCert.SerialNumber.Cmp(oldCert.SerialNumber) == 0 {
		t.Fatal("expected rotation to issue a certificate with a new serial number, not reuse the old one")
	}

	newKey := parsePEMECKey(t, result.PrivateKeyPEM)
	oldKey := clientCert.PrivateKey.(*ecdsa.PrivateKey)
	if newKey.X.Cmp(oldKey.X) == 0 && newKey.Y.Cmp(oldKey.Y) == 0 {
		t.Fatal("expected rotation to generate a fresh key pair, not reuse the old one")
	}

	// The rotated certificate must itself verify against the CA, exactly
	// like the certificate issued at initial enrollment.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(result.CACertificatePEM) {
		t.Fatal("failed to load returned CA certificate")
	}
	if _, err := newCert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("rotated certificate does not verify: %v", err)
	}
}

func TestRotateRejectsRequestWithoutClientCertificate(t *testing.T) {
	server, _, caPool := newMTLSTestServer(t)

	tlsConfig := &tls.Config{RootCAs: caPool}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := enrollment.Rotate(ctx, server.URL, tlsConfig, "collector-1"); err == nil {
		t.Fatal("expected rotation without a client certificate to fail")
	}
}

func TestRotateIgnoresCSRCommonNameInFavorOfPeerCertificate(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)

	// Build a CSR that asks for a different collector ID than the peer
	// certificate actually presents, to prove the controller derives the
	// issued certificate's identity from the verified TLS peer certificate,
	// not from anything the client claims in the request body — otherwise a
	// compromised collector could mint itself a certificate for another
	// collector's identity.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "someone-elses-identity"}}, key)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}
	body, err := json.Marshal(enrollment.RotateRequest{CSR: base64.StdEncoding.EncodeToString(csrDER)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(server.URL+"/v1/rotate", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var decoded enrollment.EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	newCert := parsePEMCertificate(t, []byte(decoded.CertificatePEM))
	if newCert.Subject.CommonName != "collector-1" {
		t.Fatalf("issued certificate common name = %q, want collector-1 (the verified peer identity), not the CSR's requested identity", newCert.Subject.CommonName)
	}
}

func TestRotateReturns501WhenEnrollmentDisabled(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/rotate", strings.NewReader(`{"csr":""}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", w.Code)
	}
}
