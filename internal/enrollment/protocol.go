package enrollment

import "time"

// EnrollRequest is the POST /v1/enroll request body. CSR is a
// base64-standard-encoded DER PKCS#10 certificate signing request; the
// private key that produced it never leaves the requesting collector.
type EnrollRequest struct {
	Token       string `json:"token"`
	CollectorID string `json:"collector_id"`
	CSR         string `json:"csr"`
}

// EnrollResponse is the POST /v1/enroll response body.
type EnrollResponse struct {
	CertificatePEM   string    `json:"certificate"`
	CACertificatePEM string    `json:"ca_certificate"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// TokenResponse is the POST /v1/enrollment-tokens response body.
type TokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
