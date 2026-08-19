package enrollment

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Filenames topo agent enroll writes an issued certificate under, and
// LoadClientTLSConfig reads them back from.
const (
	ClientCertFile = "client-cert.pem"
	ClientKeyFile  = "client-key.pem"
	CACertFile     = "ca-cert.pem"
)

// LoadClientTLSConfig builds a tls.Config for outbound mutual TLS from the
// certificate, private key, and CA certificate files topo agent enroll
// wrote to dir. The controller's own TLS server certificate is signed by
// the same CA, so this is also how the collector verifies the controller.
func LoadClientTLSConfig(dir string) (*tls.Config, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("certificate directory must be an absolute path")
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, ClientCertFile), filepath.Join(dir, ClientKeyFile))
	if err != nil {
		return nil, fmt.Errorf("load enrolled certificate: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, CACertFile))
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("CA certificate file does not contain a valid PEM certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// WriteResult writes result's certificate, private key, and CA certificate
// to dir in the layout LoadClientTLSConfig reads back, creating dir with
// owner-only permissions if it does not already exist. Both `topo agent
// enroll` and `topo agent rotate` use this to persist what the controller
// issued.
func WriteResult(dir string, result Result) error {
	if !filepath.IsAbs(dir) {
		return errors.New("certificate directory must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create certificate directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict certificate directory permissions: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ClientCertFile), result.CertificatePEM, 0o644); err != nil {
		return fmt.Errorf("write client certificate: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ClientKeyFile), result.PrivateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("write client private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, CACertFile), result.CACertificatePEM, 0o644); err != nil {
		return fmt.Errorf("write CA certificate: %w", err)
	}
	return nil
}

// CurrentCollectorID reads the certificate topo agent enroll (or a prior
// rotation) wrote to dir and returns its subject common name — the
// collector ID it was issued for. `topo agent rotate` uses this so an
// operator does not need to re-supply the collector ID by hand; it is only
// ever used to label the CSR built for the rotation request, since the
// controller itself derives the collector's identity from the peer
// certificate the TLS handshake verifies, not from the CSR.
func CurrentCollectorID(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", errors.New("certificate directory must be an absolute path")
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, ClientCertFile))
	if err != nil {
		return "", fmt.Errorf("read current certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", errors.New("client certificate file does not contain a PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse current certificate: %w", err)
	}
	return cert.Subject.CommonName, nil
}
