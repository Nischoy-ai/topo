package enrollment

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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxEnrollResponseBytes bounds the controller's enrollment response.
const maxEnrollResponseBytes = 64 << 10

// requestTimeout bounds a single enrollment HTTP round trip.
const requestTimeout = 30 * time.Second

// Result holds what a successful enrollment produced: a signed
// certificate, the private key generated for it, and the CA certificate
// the collector should trust for the controller going forward.
type Result struct {
	CertificatePEM   []byte
	PrivateKeyPEM    []byte
	CACertificatePEM []byte
	ExpiresAt        time.Time
}

// Enroll generates a fresh ECDSA P-256 key pair and certificate signing
// request for collectorID, submits it to controllerURL along with the
// one-time token, and returns the signed certificate. The private key is
// generated here and never transmitted; only the CSR — the public key plus
// a signature proving possession of the private key — crosses the network.
func Enroll(ctx context.Context, controllerURL, token, collectorID string) (Result, error) {
	if !ValidCollectorID(collectorID) {
		return Result{}, errors.New("collector ID is empty, too long, or contains control characters")
	}
	parsed, err := url.Parse(controllerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Result{}, errors.New("controller URL must be an absolute http:// or https:// URL")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Result{}, fmt.Errorf("generate collector key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: collectorID}}, key)
	if err != nil {
		return Result{}, fmt.Errorf("build certificate signing request: %w", err)
	}

	requestBody, err := json.Marshal(EnrollRequest{
		Token:       token,
		CollectorID: collectorID,
		CSR:         base64.StdEncoding.EncodeToString(csrDER),
	})
	if err != nil {
		return Result{}, fmt.Errorf("marshal enrollment request: %w", err)
	}

	enrollURL := strings.TrimRight(controllerURL, "/") + "/v1/enroll"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, enrollURL, bytes.NewReader(requestBody))
	if err != nil {
		return Result{}, fmt.Errorf("build enrollment request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: requestTimeout}
	response, err := client.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("submit enrollment request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxEnrollResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read enrollment response: %w", err)
	}
	if len(body) > maxEnrollResponseBytes {
		return Result{}, errors.New("enrollment response exceeds 65536 bytes")
	}
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("controller rejected enrollment with status %s: %s", response.Status, body)
	}

	var decoded EnrollResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Result{}, fmt.Errorf("decode enrollment response: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Result{}, fmt.Errorf("marshal collector key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return Result{
		CertificatePEM:   []byte(decoded.CertificatePEM),
		PrivateKeyPEM:    keyPEM,
		CACertificatePEM: []byte(decoded.CACertificatePEM),
		ExpiresAt:        decoded.ExpiresAt,
	}, nil
}
