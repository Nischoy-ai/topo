package controller

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
)

func testCSRBase64(t *testing.T, commonName string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: commonName}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func newEnrollmentTestServer(t *testing.T) *Server {
	t.Helper()
	ca, err := enrollment.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(store.NewMemory(), nil, "secret")
	s.CA = ca
	s.Tokens = enrollment.NewTokenStore()
	return s
}

func mintToken(t *testing.T, h http.Handler) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/enrollment-tokens", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("mint token status %d: %s", w.Code, w.Body.String())
	}
	var resp enrollment.TokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Token
}

func TestEnrollmentDisabledByDefault(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()

	r := httptest.NewRequest(http.MethodPost, "/v1/enrollment-tokens", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("mint token status = %d, want 501", w.Code)
	}

	r = httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewBufferString(`{}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("enroll status = %d, want 501", w.Code)
	}
}

func TestCreateEnrollmentTokenRequiresAuth(t *testing.T) {
	h := newEnrollmentTestServer(t).Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/enrollment-tokens", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestEnrollFullFlow(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	token := mintToken(t, h)

	body, _ := json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-1", CSR: testCSRBase64(t, "collector-1")})
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll status %d: %s", w.Code, w.Body.String())
	}

	var resp enrollment.EnrollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(resp.CACertificatePEM)) {
		t.Fatal("failed to load returned CA certificate")
	}
	block, _ := pem.Decode([]byte(resp.CertificatePEM))
	if block == nil {
		t.Fatal("expected a PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "collector-1" {
		t.Fatalf("common name = %q", cert.Subject.CommonName)
	}
	if resp.SerialNumber != enrollment.CertificateSerial(cert) {
		t.Fatalf("response serial = %q, certificate serial = %q", resp.SerialNumber, enrollment.CertificateSerial(cert))
	}
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("issued certificate does not verify: %v", err)
	}

	// The token was single-use.
	body, _ = json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-2", CSR: testCSRBase64(t, "collector-2")})
	r = httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d, want 401", w.Code)
	}
}

func TestEnrollRejectsMalformedCSRWithoutBurningToken(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	token := mintToken(t, h)

	body, _ := json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-1", CSR: "not-valid-base64!!"})
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	// The token must still be usable: a malformed request must not have
	// consumed it.
	body, _ = json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-1", CSR: testCSRBase64(t, "collector-1")})
	r = httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("retry status %d: %s", w.Code, w.Body.String())
	}
}

func TestEnrollRejectsInvalidToken(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	body, _ := json.Marshal(enrollment.EnrollRequest{Token: "bogus", CollectorID: "collector-1", CSR: testCSRBase64(t, "collector-1")})
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestEnrollClientEndToEnd(t *testing.T) {
	s := newEnrollmentTestServer(t)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()

	tokenRequest := httptest.NewRequest(http.MethodPost, "/v1/enrollment-tokens", nil)
	tokenRequest.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, tokenRequest)
	var tokenResp enrollment.TokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &tokenResp); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := enrollment.Enroll(ctx, httpServer.URL, tokenResp.Token, "collector-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PrivateKeyPEM) == 0 {
		t.Fatal("expected a generated private key")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(result.CACertificatePEM) {
		t.Fatal("failed to load CA certificate")
	}
	block, _ := pem.Decode(result.CertificatePEM)
	if block == nil {
		t.Fatal("expected a PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "collector-1" {
		t.Fatalf("common name = %q", cert.Subject.CommonName)
	}

	keyBlock, _ := pem.Decode(result.PrivateKeyPEM)
	if keyBlock == nil {
		t.Fatal("expected a PEM block for the private key")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !key.PublicKey.Equal(cert.PublicKey.(*ecdsa.PublicKey)) {
		t.Fatal("issued certificate's public key does not match the generated private key")
	}
}

func TestEnrollClientRejectsNonHTTPURL(t *testing.T) {
	if _, err := enrollment.Enroll(context.Background(), "ftp://example.test", "token", "collector-1", nil); err == nil {
		t.Fatal("expected a non-http(s) controller URL to be rejected")
	}
}

func TestEnrollRejectsInvalidCollectorID(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	token := mintToken(t, h)
	body, _ := json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "", CSR: testCSRBase64(t, "collector-1")})
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
