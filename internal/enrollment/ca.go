// Package enrollment implements Topo Hub's collector certificate
// authority: generating and persisting a signing CA, issuing short-lived
// single-use enrollment tokens, and signing collector certificate signing
// requests (CSRs). A collector's private key is generated on the collector
// and never transmitted; only the CSR — a public key plus a signature
// proving possession of the matching private key — crosses the network.
package enrollment

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	caKeyFile  = "ca-key.pem"
	caCertFile = "ca-cert.pem"
	caValidity = 10 * 365 * 24 * time.Hour

	// DefaultCertificateTTL bounds an issued or rotated collector
	// certificate's lifetime. A bounded TTL remains defense in depth beside
	// serial-specific revocation; `topo agent rotate` renews it before expiry.
	DefaultCertificateTTL = 90 * 24 * time.Hour

	// DefaultServerCertificateTTL bounds the controller's own TLS server
	// certificate. It is regenerated fresh on every `topo serve` start
	// rather than persisted, so this is deliberately longer than
	// DefaultCertificateTTL: a long-running controller process does not
	// yet renew it while running (that is part of the certificate
	// rotation follow-on slice), so it must outlive the process, not just
	// bound exposure the way a collector certificate's TTL does.
	DefaultServerCertificateTTL = 365 * 24 * time.Hour

	// clockSkewTolerance backdates NotBefore slightly so a certificate is
	// immediately valid even if the collector's and controller's clocks
	// are not perfectly synchronized.
	clockSkewTolerance = 5 * time.Minute

	maxCollectorIDBytes = 253 // DNS-hostname-length convention
)

// CA holds Topo Hub's collector certificate signing authority.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// LoadOrCreateCA loads a CA persisted at dir, or generates and persists a
// new one if none exists yet. dir must be an absolute path so behavior does
// not depend on the working directory. The private key is written with
// owner-only permissions, matching how every other private key in this
// project (SSH, TLS) is protected: by filesystem permissions, not a second
// layer of application encryption whose own key would need protecting the
// same way.
func LoadOrCreateCA(dir string) (*CA, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("CA directory must be an absolute path")
	}
	keyPath := filepath.Join(dir, caKeyFile)
	certPath := filepath.Join(dir, caCertFile)

	_, err := os.Stat(keyPath)
	switch {
	case err == nil:
		return loadCA(keyPath, certPath)
	case os.IsNotExist(err):
		// fall through to generation below
	default:
		return nil, fmt.Errorf("inspect CA key %q: %w", keyPath, err)
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create CA directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("restrict CA directory permissions: %w", err)
	}
	ca, err := generateCA()
	if err != nil {
		return nil, err
	}
	if err := ca.persist(keyPath, certPath); err != nil {
		return nil, err
	}
	return ca, nil
}

func generateCA() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Topo Hub Collector CA"},
		NotBefore:             now.Add(-clockSkewTolerance),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse generated CA certificate: %w", err)
	}
	return &CA{cert: cert, key: key}, nil
}

func (ca *CA) persist(keyPath, certPath string) error {
	keyDER, err := x509.MarshalECPrivateKey(ca.key)
	if err != nil {
		return fmt.Errorf("marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("write CA key: %w", err)
	}
	if err := os.WriteFile(certPath, ca.CACertPEM(), 0o644); err != nil {
		return fmt.Errorf("write CA certificate: %w", err)
	}
	return nil
}

func loadCA(keyPath, certPath string) (*CA, error) {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read CA key: %w", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("CA key file does not contain a PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("CA certificate file does not contain a PEM block")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	return &CA{cert: cert, key: key}, nil
}

// CACertPEM returns the CA's own certificate, PEM-encoded, for handing to
// enrolling collectors so they can verify the controller in turn.
func (ca *CA) CACertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

// ParseCSR decodes a DER-encoded PKCS#10 certificate signing request and
// verifies its self-signature, proving the requester holds the private key
// matching the request's public key. It does not consult the CA or any
// token; callers should validate an enrollment token separately before
// calling Sign, so a malformed request never consumes a valid token.
func ParseCSR(der []byte) (*x509.CertificateRequest, error) {
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, fmt.Errorf("parse certificate signing request: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("certificate signing request signature is invalid: %w", err)
	}
	return csr, nil
}

// ValidCollectorID reports whether id is safe to use as a certificate
// common name and store as a token/log identifier: non-empty, bounded, and
// free of control characters.
func ValidCollectorID(id string) bool {
	if id == "" || len(id) > maxCollectorIDBytes {
		return false
	}
	if strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Sign issues a short-lived client-authentication certificate for csr,
// valid for ttl, with commonName as its subject. commonName is the
// collector ID and should already be validated with ValidCollectorID.
func (ca *CA) Sign(csr *x509.CertificateRequest, commonName string, ttl time.Duration) ([]byte, error) {
	if ttl <= 0 {
		return nil, errors.New("certificate ttl must be positive")
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-clockSkewTolerance),
		NotAfter:     now.Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// IssueServerCertificate issues a fresh TLS server certificate for the
// controller itself, signed by the same CA that signs collector client
// certificates, so an enrolled collector can verify the controller using
// the CA certificate it already received during enrollment. sans must
// contain at least one DNS name or IP address the controller is reachable
// at; entries that parse as an IP address become IP SANs, everything else
// becomes a DNS SAN.
func (ca *CA) IssueServerCertificate(sans []string, ttl time.Duration) (certPEM, keyPEM []byte, err error) {
	if ttl <= 0 {
		return nil, nil, errors.New("certificate ttl must be positive")
	}
	if len(sans) == 0 {
		return nil, nil, errors.New("server certificate requires at least one subject alternative name")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate server key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Topo Hub"},
		NotBefore:    now.Add(-clockSkewTolerance),
		NotAfter:     now.Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, san := range sans {
		if ip := net.ParseIP(san); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, san)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, nil, fmt.Errorf("sign server certificate: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal server key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial number: %w", err)
	}
	return serial, nil
}
