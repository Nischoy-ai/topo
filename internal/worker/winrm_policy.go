package worker

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/netip"
	"regexp"
)

var passwordUsername = regexp.MustCompile(`^[A-Za-z0-9._@\\-]{1,256}$`)

func safePasswordUsername(value string) bool { return passwordUsername.MatchString(value) }

type WinRMStartupConfig struct {
	Allowlist   []netip.Prefix
	TrustDigest string
	RootCAs     *x509.CertPool
}

// LoadWinRMStartupConfig loads explicit deployment-owned roots. The HTTPS
// connection must still verify the target IP SAN; trusting a root cannot bypass it.
func LoadWinRMStartupConfig(allowlistPath, rootsPath string) (WinRMStartupConfig, error) {
	body, err := readBoundedRegularFile(allowlistPath, maxSSHAllowlistBytes, "WinRM target allowlist")
	if err != nil {
		return WinRMStartupConfig{}, err
	}
	allowlist, err := parseSSHAllowlist(body)
	if err != nil {
		return WinRMStartupConfig{}, errors.New("WinRM allowlist must contain bounded canonical IPv4 CIDRs")
	}
	roots, err := readBoundedRegularFile(rootsPath, 1<<20, "WinRM CA certificates")
	if err != nil {
		return WinRMStartupConfig{}, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(roots) {
		return WinRMStartupConfig{}, errors.New("WinRM CA file contains no usable certificates")
	}
	digest := sha256.Sum256(roots)
	return WinRMStartupConfig{Allowlist: allowlist, RootCAs: pool, TrustDigest: hex.EncodeToString(digest[:])}, nil
}
