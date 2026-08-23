package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
)

// newMTLSTestServer builds a Server with enrollment enabled, issues it a
// TLS server certificate and an enrolled client certificate from the same
// CA, and returns an httptest.Server configured to require and verify
// client certificates, mirroring `topo serve -ca-dir ... -mtls`.
func newMTLSTestServer(t *testing.T) (httpServer *httptest.Server, clientCert tls.Certificate, caPool *x509.CertPool) {
	t.Helper()
	s := newEnrollmentTestServer(t)

	serverCertPEM, serverKeyPEM, err := s.CA.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "collector-1"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := enrollment.ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	clientCertPEM, err := s.CA.Sign(csr, "collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(s.CA.CACertPEM()) {
		t.Fatal("failed to load CA cert into pool")
	}

	// VerifyClientCertIfGiven, matching `topo serve -mtls`: the TLS layer
	// must accept a handshake with no client certificate at all (a
	// collector's first-ever request, POST /v1/enroll, has none to
	// present), leaving per-endpoint enforcement to the authorization middleware.
	server := httptest.NewUnstartedServer(s.Handler())
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return server, cert, pool
}

func TestCollectorAuthAcceptsVerifiedClientCertificateWithoutBearerKey(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}

	// Deliberately no Authorization header: the certificate alone must be
	// sufficient for collector data-plane traffic.
	resp, err := client.Post(server.URL+"/v1/heartbeats", "application/json", strings.NewReader(`{"schema_version":"v1alpha1","collector_id":"collector-1","site_id":"site-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
}

func TestAdminAuthRejectsVerifiedCollectorCertificateWithoutBearerKey(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}

	resp, err := client.Get(server.URL + "/v1/assets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAdminAuthAcceptsBearerKeyAlongsideCollectorCertificate(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/assets", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAuthRejectsRequestWithoutClientCertificateOrBearerKey(t *testing.T) {
	server, _, caPool := newMTLSTestServer(t)

	// The TLS handshake itself must succeed with no client certificate
	// presented (POST /v1/enroll depends on that), so rejection of an
	// unauthenticated request to a protected endpoint happens at the
	// application layer, as a 401, not as a handshake failure.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caPool}}}
	resp, err := client.Get(server.URL + "/v1/assets")
	if err != nil {
		t.Fatalf("expected the TLS handshake to succeed without a client certificate, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestEnrollSucceedsWithoutClientCertificateUnderMTLS(t *testing.T) {
	server, _, caPool := newMTLSTestServer(t)

	// A collector's very first request has no certificate yet: it must be
	// able to reach POST /v1/enroll (authenticated by a one-time token,
	// not TLS identity) over the same mTLS listener that will require a
	// certificate for every request after enrollment.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caPool}}}
	resp, err := client.Post(server.URL+"/v1/enroll", "application/json", strings.NewReader(`{"token":"bogus","collector_id":"c1","csr":""}`))
	if err != nil {
		t.Fatalf("expected the TLS handshake to succeed without a client certificate, got: %v", err)
	}
	defer resp.Body.Close()
	// The token is bogus, so enrollment itself is rejected, but that
	// rejection must come from the application layer (400/401), never
	// from the TLS handshake refusing the connection outright.
	if resp.StatusCode == 0 {
		t.Fatal("expected an HTTP response, got none")
	}
}

// TestEnrollClientTrustsPinnedBootstrapCACertificate proves the bootstrap
// chicken-and-egg problem a native `-mtls` controller creates is actually
// solvable: the controller's TLS certificate is self-signed by its own
// enrollment CA, which system trust roots do not recognize, so
// enrollment.Enroll must be able to pin that CA certificate (distributed
// out-of-band, exactly like the token) to complete its very first request.
func TestEnrollClientTrustsPinnedBootstrapCACertificate(t *testing.T) {
	s := newEnrollmentTestServer(t)

	serverCertPEM, serverKeyPEM, err := s.CA.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.Handler())
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	token := mintToken(t, s.Handler(), "collector-bootstrap")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := enrollment.Enroll(ctx, server.URL, token, "collector-bootstrap", s.CA.CACertPEM())
	if err != nil {
		t.Fatalf("enroll with pinned bootstrap CA certificate: %v", err)
	}
	if len(result.CertificatePEM) == 0 {
		t.Fatal("expected a signed certificate")
	}
}

func TestEnrollClientRejectsUntrustedServerWithoutBootstrapCACert(t *testing.T) {
	s := newEnrollmentTestServer(t)

	serverCertPEM, serverKeyPEM, err := s.CA.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.Handler())
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	token := mintToken(t, s.Handler(), "collector-bootstrap")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := enrollment.Enroll(ctx, server.URL, token, "collector-bootstrap", nil); err == nil {
		t.Fatal("expected enrollment without a pinned CA certificate to fail against a self-signed controller")
	}
}
