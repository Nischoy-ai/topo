package enrollment

import (
	"bytes"
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

// Result holds what a successful enrollment produced: a signed certificate
// and its canonical serial, the private key generated for it, and the CA
// certificate the collector should trust for the controller going forward.
type Result struct {
	CertificatePEM   []byte
	PrivateKeyPEM    []byte
	CACertificatePEM []byte
	SerialNumber     string
	ExpiresAt        time.Time
}

// Enroll generates a fresh ECDSA P-256 key pair and certificate signing
// request for collectorID, submits it to controllerURL along with the
// one-time token, and returns the signed certificate. The private key is
// generated here and never transmitted; only the CSR — the public key plus
// a signature proving possession of the private key — crosses the network.
//
// bootstrapCACertPEM is optional. When controllerURL runs `topo serve -mtls`,
// the controller presents a certificate signed by its own enrollment CA,
// which system trust roots do not recognize; pass that CA's certificate
// (distributed out-of-band alongside the enrollment token, the same way a
// collector already receives the token) to pin the connection to it instead
// of relying on system trust roots. Leave it nil when the controller's TLS
// is terminated by a reverse proxy with a publicly-trusted certificate, or
// when the controller runs over plain HTTP within a trusted network.
func Enroll(ctx context.Context, controllerURL, token, collectorID string, bootstrapCACertPEM []byte) (Result, error) {
	if !ValidCollectorID(collectorID) {
		return Result{}, errors.New("collector ID is empty, too long, or contains control characters")
	}
	if err := validControllerURL(controllerURL); err != nil {
		return Result{}, err
	}

	key, csrDER, err := generateKeyAndCSR(collectorID)
	if err != nil {
		return Result{}, err
	}
	requestBody, err := json.Marshal(EnrollRequest{
		Token:       token,
		CollectorID: collectorID,
		CSR:         base64.StdEncoding.EncodeToString(csrDER),
	})
	if err != nil {
		return Result{}, fmt.Errorf("marshal enrollment request: %w", err)
	}

	httpClient := &http.Client{Timeout: requestTimeout}
	if len(bootstrapCACertPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(bootstrapCACertPEM) {
			return Result{}, errors.New("bootstrap CA certificate does not contain a valid PEM certificate")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		httpClient.Transport = transport
	}

	decoded, err := submitCSR(ctx, httpClient, strings.TrimRight(controllerURL, "/")+"/v1/enroll", requestBody)
	if err != nil {
		return Result{}, fmt.Errorf("enroll: %w", err)
	}
	return resultFromResponse(key, decoded)
}

// Rotate renews collectorID's certificate before it expires, authenticated
// by the certificate already loaded into tlsConfig rather than a new
// enrollment token. The controller derives the collector's identity from
// the peer certificate the TLS handshake itself verified, not from
// anything in the request body, so a collector can only ever rotate its
// own certificate. tlsConfig must present the collector's current,
// still-valid certificate — build one with LoadClientTLSConfig against the
// same directory `topo agent enroll` wrote. A certificate that has already
// expired cannot rotate itself this way; re-enroll with a fresh token
// instead.
//
// This requires the controller to run `topo serve -mtls`: the rotation
// endpoint has no bearer-API-key fallback, because accepting one would let
// anyone holding the shared key mint a certificate for any collector ID,
// defeating the point of per-collector identity.
func Rotate(ctx context.Context, controllerURL string, tlsConfig *tls.Config, collectorID string) (Result, error) {
	if !ValidCollectorID(collectorID) {
		return Result{}, errors.New("collector ID is empty, too long, or contains control characters")
	}
	if err := validControllerURL(controllerURL); err != nil {
		return Result{}, err
	}
	if tlsConfig == nil {
		return Result{}, errors.New("rotation requires a TLS config presenting the collector's current certificate")
	}

	key, csrDER, err := generateKeyAndCSR(collectorID)
	if err != nil {
		return Result{}, err
	}
	requestBody, err := json.Marshal(RotateRequest{CSR: base64.StdEncoding.EncodeToString(csrDER)})
	if err != nil {
		return Result{}, fmt.Errorf("marshal rotation request: %w", err)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	httpClient := &http.Client{Timeout: requestTimeout, Transport: transport}

	decoded, err := submitCSR(ctx, httpClient, strings.TrimRight(controllerURL, "/")+"/v1/rotate", requestBody)
	if err != nil {
		return Result{}, fmt.Errorf("rotate: %w", err)
	}
	return resultFromResponse(key, decoded)
}

func validControllerURL(controllerURL string) error {
	parsed, err := url.Parse(controllerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("controller URL must be an absolute http:// or https:// URL")
	}
	return nil
}

// generateKeyAndCSR generates a fresh ECDSA P-256 key pair and a
// self-signed certificate signing request for collectorID. The private key
// never leaves the caller; only the returned CSR — the public key plus a
// signature proving possession of the private key — is meant to cross the
// network.
func generateKeyAndCSR(collectorID string) (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate collector key: %w", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: collectorID}}, key)
	if err != nil {
		return nil, nil, fmt.Errorf("build certificate signing request: %w", err)
	}
	return key, csrDER, nil
}

// submitCSR POSTs requestBody to requestURL and decodes an EnrollResponse
// (both /v1/enroll and /v1/rotate share this response shape).
func submitCSR(ctx context.Context, httpClient *http.Client, requestURL string, requestBody []byte) (EnrollResponse, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(requestBody))
	if err != nil {
		return EnrollResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return EnrollResponse{}, fmt.Errorf("submit request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxEnrollResponseBytes+1))
	if err != nil {
		return EnrollResponse{}, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxEnrollResponseBytes {
		return EnrollResponse{}, errors.New("response exceeds 65536 bytes")
	}
	if response.StatusCode != http.StatusOK {
		return EnrollResponse{}, fmt.Errorf("controller rejected request with status %s: %s", response.Status, body)
	}

	var decoded EnrollResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return EnrollResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return decoded, nil
}

func resultFromResponse(key *ecdsa.PrivateKey, decoded EnrollResponse) (Result, error) {
	serial, err := CertificateSerialFromPEM([]byte(decoded.CertificatePEM))
	if err != nil {
		return Result{}, fmt.Errorf("decode issued certificate: %w", err)
	}
	if decoded.SerialNumber != "" {
		normalized, err := NormalizeCertificateSerial(decoded.SerialNumber)
		if err != nil || normalized != serial {
			return Result{}, errors.New("controller response serial_number does not match the issued certificate")
		}
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
		SerialNumber:     serial,
		ExpiresAt:        decoded.ExpiresAt,
	}, nil
}
