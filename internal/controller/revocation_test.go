package controller

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
)

func issueCollectorCertificate(t *testing.T, s *Server, collectorID string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: collectorID}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := enrollment.ParseCSR(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := s.CA.Sign(csr, collectorID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return parsePEMCertificate(t, certPEM)
}

func requestWithCertificate(cert *x509.Certificate, method, target string, body []byte) *http.Request {
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	return r
}

func revokeCertificate(t *testing.T, h http.Handler, serial, reason string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(certificateRevocationRequest{SerialNumber: serial, Reason: reason})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/certificate-revocations", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCertificateRevocationCreateListAndAuditAreIdempotent(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	w := revokeCertificate(t, h, "0x00:AB:CD", "private key copied from collector")
	if w.Code != http.StatusCreated {
		t.Fatalf("first revoke status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var first store.CertificateRevocation
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.SerialNumber != "abcd" || first.Reason != "private key copied from collector" || first.RevokedAt.IsZero() {
		t.Fatalf("unexpected revocation: %#v", first)
	}

	w = revokeCertificate(t, h, "ABCD", "a later reason must not replace the incident record")
	if w.Code != http.StatusOK {
		t.Fatalf("repeat revoke status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var repeated store.CertificateRevocation
	if err := json.Unmarshal(w.Body.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Reason != first.Reason || !repeated.RevokedAt.Equal(first.RevokedAt) {
		t.Fatalf("repeat revoke changed immutable record: first %#v, repeat %#v", first, repeated)
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/certificate-revocations", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []store.CertificateRevocation `json:"items"`
		Count int                           `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Count != 1 || len(listed.Items) != 1 || listed.Items[0].SerialNumber != "abcd" {
		t.Fatalf("unexpected list response: %#v", listed)
	}
	entries, err := s.Store.ListAuditEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	certificateRevokedCount := 0
	for _, entry := range entries {
		if entry.Action == "certificate_revoked" {
			certificateRevokedCount++
			if entry.Detail["serial_number"] != "abcd" || entry.Detail["reason"] != first.Reason {
				t.Fatalf("unexpected revocation audit detail: %#v", entry.Detail)
			}
		}
	}
	if certificateRevokedCount != 1 {
		t.Fatalf("certificate_revoked audit entries = %d, want 1", certificateRevokedCount)
	}
}

func TestCertificateRevocationEndpointsRequireOperatorAuth(t *testing.T) {
	h := newEnrollmentTestServer(t).Handler()
	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/certificate-revocations"},
		{method: http.MethodGet, path: "/v1/certificate-revocations"},
	} {
		r := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", endpoint.method, endpoint.path, w.Code)
		}
	}
}

func TestCertificateRevocationRejectsInvalidInput(t *testing.T) {
	h := newEnrollmentTestServer(t).Handler()
	for name, body := range map[string]string{
		"zero serial":       `{"serial_number":"0","reason":"incident"}`,
		"non-hex serial":    `{"serial_number":"not-hex","reason":"incident"}`,
		"empty reason":      `{"serial_number":"1","reason":""}`,
		"surrounding space": `{"serial_number":"1","reason":" incident"}`,
		"control character": `{"serial_number":"1","reason":"incident\ncontinued"}`,
		"unknown field":     `{"serial_number":"1","reason":"incident","unrevoke":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/certificate-revocations", bytes.NewBufferString(body))
			r.Header.Set("Authorization", "Bearer secret")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRevokedCertificateCannotUseCollectorPlaneOrRotate(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	cert := issueCollectorCertificate(t, s, "collector-1")
	if w := revokeCertificate(t, h, enrollment.CertificateSerial(cert), "collector stolen"); w.Code != http.StatusCreated {
		t.Fatalf("revoke status = %d: %s", w.Code, w.Body.String())
	}

	collectorEndpoints := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "observation", method: http.MethodPost, path: "/v1/observations", body: []byte(`{"schema_version":"v1alpha1","observation_id":"o1","collector_id":"collector-1","observed_at":"2026-08-21T00:00:00Z","assets":[]}`)},
		{name: "heartbeat", method: http.MethodPost, path: "/v1/heartbeats", body: []byte(`{"schema_version":"v1alpha1","collector_id":"collector-1","site_id":"site-a"}`)},
		{name: "job poll", method: http.MethodGet, path: "/v1/jobs?collector_id=collector-1"},
		{name: "job result", method: http.MethodPost, path: "/v1/jobs/missing/result", body: []byte(`{"collector_id":"collector-1","success":true}`)},
	}
	for _, endpoint := range collectorEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			r := requestWithCertificate(cert, endpoint.method, endpoint.path, endpoint.body)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
			}
		})
	}

	rotateBody, err := json.Marshal(enrollment.RotateRequest{CSR: testCSRBase64(t, "collector-1")})
	if err != nil {
		t.Fatal(err)
	}
	r := requestWithCertificate(cert, http.MethodPost, "/v1/rotate", rotateBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("rotate status = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestBearerFallbackDoesNotTrustRevokedCertificateIdentity(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	cert := issueCollectorCertificate(t, s, "revoked-collector")
	if w := revokeCertificate(t, h, enrollment.CertificateSerial(cert), "credential exposed"); w.Code != http.StatusCreated {
		t.Fatalf("revoke status = %d: %s", w.Code, w.Body.String())
	}
	r := requestWithCertificate(cert, http.MethodPost, "/v1/observations", []byte(`{"schema_version":"v1alpha1","observation_id":"o1","collector_id":"bearer-collector","observed_at":"2026-08-21T00:00:00Z","assets":[]}`))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("bearer fallback status = %d, want 202: %s", w.Code, w.Body.String())
	}
	observations, err := s.Store.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].CollectorID != "bearer-collector" {
		t.Fatalf("revoked certificate supplied identity under bearer fallback: %#v", observations)
	}
}

func TestReEnrollmentRecoversFromRevokedCertificate(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	oldCert := issueCollectorCertificate(t, s, "collector-1")
	oldSerial := enrollment.CertificateSerial(oldCert)
	if w := revokeCertificate(t, h, oldSerial, "credential exposed"); w.Code != http.StatusCreated {
		t.Fatalf("revoke status = %d: %s", w.Code, w.Body.String())
	}

	token := mintToken(t, h)
	body, err := json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-1", CSR: testCSRBase64(t, "collector-1")})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("re-enroll status = %d: %s", w.Code, w.Body.String())
	}
	var enrolled enrollment.EnrollResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	newCert := parsePEMCertificate(t, []byte(enrolled.CertificatePEM))
	if enrolled.SerialNumber == oldSerial || enrollment.CertificateSerial(newCert) != enrolled.SerialNumber {
		t.Fatalf("re-enrollment did not produce a distinct, correctly reported serial: old %s, response %#v", oldSerial, enrolled)
	}

	r = requestWithCertificate(newCert, http.MethodPost, "/v1/heartbeats", []byte(`{"schema_version":"v1alpha1","collector_id":"collector-1","site_id":"site-a"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("new certificate heartbeat status = %d, want 202: %s", w.Code, w.Body.String())
	}
}

type revocationCheckErrorRepository struct {
	store.Repository
}

func (revocationCheckErrorRepository) IsCertificateRevoked(context.Context, string) (bool, error) {
	return false, errors.New("storage unavailable")
}

func TestCertificateRevocationLookupFailureFailsClosed(t *testing.T) {
	repo := revocationCheckErrorRepository{Repository: store.NewMemory()}
	h := New(repo, nil, "secret").Handler()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "collector-1"}}
	r := requestWithCertificate(cert, http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", w.Code, w.Body.String())
	}
}

type blockingRevocationRepository struct {
	store.Repository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingRevocationRepository) IsCertificateRevoked(ctx context.Context, serial string) (bool, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return r.Repository.IsCertificateRevoked(ctx, serial)
}

func TestRotationAlreadyAuthorizedCompletesBeforeCompetingRevocation(t *testing.T) {
	s := newEnrollmentTestServer(t)
	blocking := &blockingRevocationRepository{
		Repository: store.NewMemory(),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	s.Store = blocking
	h := s.Handler()
	cert := issueCollectorCertificate(t, s, "collector-1")
	rotateBody, err := json.Marshal(enrollment.RotateRequest{CSR: testCSRBase64(t, "collector-1")})
	if err != nil {
		t.Fatal(err)
	}

	rotateDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := requestWithCertificate(cert, http.MethodPost, "/v1/rotate", rotateBody)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		rotateDone <- w
	}()
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("rotation did not reach revocation authorization")
	}

	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		revokeDone <- revokeCertificate(t, h, enrollment.CertificateSerial(cert), "race ordering test")
	}()
	select {
	case w := <-revokeDone:
		t.Fatalf("revocation completed while rotation held the authorization lock: %d %s", w.Code, w.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	close(blocking.release)

	rotateResponse := <-rotateDone
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("rotation status = %d, want 200: %s", rotateResponse.Code, rotateResponse.Body.String())
	}
	revokeResponse := <-revokeDone
	if revokeResponse.Code != http.StatusCreated {
		t.Fatalf("revocation status = %d, want 201: %s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

var _ store.Repository = revocationCheckErrorRepository{}
var _ store.Repository = (*blockingRevocationRepository)(nil)
