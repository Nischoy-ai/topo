package enrollment

import (
	"crypto/tls"
	"crypto/x509"
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
