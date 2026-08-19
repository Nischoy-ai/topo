package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestSenderSendPostsSingleEnvelope(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody model.ObservationEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "test-api-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope("obs-1")
	if err := sender.Send(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/observations" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotBody.ObservationID != envelope.ObservationID {
		t.Fatalf("observation id = %q", gotBody.ObservationID)
	}
}

func TestSenderServerErrorIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testEnvelope("obs-1"))
	var delivery *DeliveryError
	if !errors.As(err, &delivery) {
		t.Fatalf("error = %v, want *DeliveryError", err)
	}
	if !delivery.Retryable {
		t.Fatal("expected a 503 to be retryable")
	}
}

func TestSenderClientErrorIsNotRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("invalid observation"))
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testEnvelope("obs-1"))
	var delivery *DeliveryError
	if !errors.As(err, &delivery) {
		t.Fatalf("error = %v, want *DeliveryError", err)
	}
	if delivery.Retryable {
		t.Fatal("expected a 422 to be non-retryable")
	}
	if !strings.Contains(err.Error(), "invalid observation") {
		t.Fatalf("error = %v", err)
	}
}

func TestSenderHonorsContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer func() {
		close(blocked)
		server.Close()
	}()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sender.Send(ctx, testEnvelope("obs-1")); err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestNewSenderRejectsInvalidURL(t *testing.T) {
	if _, err := NewSender("not-a-url", "", nil); err == nil {
		t.Fatal("expected an invalid controller URL to be rejected")
	}
	if _, err := NewSender("ftp://example.test", "", nil); err == nil {
		t.Fatal("expected a non-http(s) scheme to be rejected")
	}
}

// setupMTLSTest builds a CA, a server certificate for 127.0.0.1, and a
// client certificate for "collector-1", writing the client certificate,
// key, and CA certificate to a temp directory in the layout
// enrollment.LoadClientTLSConfig expects (mirroring what `topo agent
// enroll` produces), so the sender's outbound mTLS is exercised through
// the same code path a real enrolled agent would use.
func setupMTLSTest(t *testing.T) (serverTLSConfig *tls.Config, caPool *x509.CertPool, clientCertDir string) {
	t.Helper()
	ca, err := enrollment.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	serverCertPEM, serverKeyPEM, err := ca.IssueServerCertificate([]string{"127.0.0.1"}, time.Hour)
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
	clientCertPEM, err := ca.Sign(csr, "collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.CACertPEM()) {
		t.Fatal("failed to load CA cert into pool")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, enrollment.ClientCertFile), clientCertPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, enrollment.ClientKeyFile), clientKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, enrollment.CACertFile), ca.CACertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, pool, dir
}

func TestSenderDeliversOverMutualTLS(t *testing.T) {
	serverTLSConfig, _, clientCertDir := setupMTLSTest(t)

	var gotCommonName string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) > 0 {
			gotCommonName = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	clientTLSConfig, err := enrollment.LoadClientTLSConfig(clientCertDir)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := NewSender(server.URL, "", clientTLSConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), testEnvelope("obs-1")); err != nil {
		t.Fatal(err)
	}
	if gotCommonName != "collector-1" {
		t.Fatalf("peer certificate common name = %q, want collector-1", gotCommonName)
	}
}

func TestSenderRejectedWithoutClientCertificate(t *testing.T) {
	serverTLSConfig, caPool, _ := setupMTLSTest(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	server.TLS = serverTLSConfig
	server.StartTLS()
	defer server.Close()

	// A client that trusts the CA for the server side but presents no
	// client certificate of its own must be rejected by the server's mTLS
	// requirement.
	sender, err := NewSender(server.URL, "", &tls.Config{RootCAs: caPool})
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), testEnvelope("obs-1")); err == nil {
		t.Fatal("expected the server to reject a connection without a client certificate")
	}
}
