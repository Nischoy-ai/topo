// Package vault resolves credential values from a HashiCorp Vault KV version
// 2 secrets engine over a bounded, cancellable HTTPS client. It never accepts
// a token as a literal CLI argument and never places resolved credential
// bytes in an error.
package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// maxResponseBytes bounds the Vault HTTP response body, matching the
	// resolved-credential bound used by the env: and file: providers.
	maxResponseBytes = 1 << 20

	// requestTimeout bounds a single Vault HTTP round trip.
	requestTimeout = 15 * time.Second
)

// ErrUnavailable indicates that a referenced secret, secret version, or field
// is not present. Callers may ignore this error only for an explicitly
// optional credential.
var ErrUnavailable = errors.New("vault credential is unavailable")

// Config holds the connection settings for a Vault KV version 2 credential
// provider. Values come from the standard Vault environment variables so
// operators can reuse existing Vault tooling and authentication.
type Config struct {
	// Address is the Vault API base URL, for example https://vault:8200.
	Address string
	// Namespace is an optional Vault Enterprise namespace.
	Namespace string
	// Mount is the KV version 2 secrets engine mount point. It defaults to
	// "secret" and is operator configuration, not part of a credential
	// reference, so one deployment resolves all vault: references against
	// one dedicated mount.
	Mount string
	// Token authenticates to Vault. It is never accepted as a CLI argument;
	// ConfigFromEnvironment reads it from VAULT_TOKEN or VAULT_TOKEN_FILE.
	Token string
	// CACertPath optionally names a PEM file trusted in addition to the
	// system trust store. Server identity verification is always enabled;
	// Topo never disables TLS certificate verification.
	CACertPath string
}

// ConfigFromEnvironment reads Config from the standard Vault environment
// variables: VAULT_ADDR, VAULT_NAMESPACE, VAULT_MOUNT, VAULT_TOKEN,
// VAULT_TOKEN_FILE, and VAULT_CACERT.
func ConfigFromEnvironment() (Config, error) {
	address := strings.TrimSpace(os.Getenv("VAULT_ADDR"))
	if address == "" {
		return Config{}, errors.New("VAULT_ADDR is not set")
	}
	if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		return Config{}, errors.New("VAULT_ADDR must be an http:// or https:// URL")
	}

	token, err := tokenFromEnvironment()
	if err != nil {
		return Config{}, err
	}

	mount := strings.TrimSpace(os.Getenv("VAULT_MOUNT"))
	if mount == "" {
		mount = "secret"
	}

	return Config{
		Address:    strings.TrimRight(address, "/"),
		Namespace:  strings.TrimSpace(os.Getenv("VAULT_NAMESPACE")),
		Mount:      strings.Trim(mount, "/"),
		Token:      token,
		CACertPath: strings.TrimSpace(os.Getenv("VAULT_CACERT")),
	}, nil
}

func tokenFromEnvironment() (string, error) {
	tokenFile := strings.TrimSpace(os.Getenv("VAULT_TOKEN_FILE"))
	token := strings.TrimSpace(os.Getenv("VAULT_TOKEN"))
	switch {
	case tokenFile != "" && token != "":
		return "", errors.New("VAULT_TOKEN and VAULT_TOKEN_FILE cannot both be set")
	case tokenFile != "":
		if !filepath.IsAbs(tokenFile) {
			return "", errors.New("VAULT_TOKEN_FILE must be an absolute path")
		}
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("read VAULT_TOKEN_FILE: %w", err)
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" {
			return "", errors.New("VAULT_TOKEN_FILE is empty")
		}
		return trimmed, nil
	case token != "":
		return token, nil
	default:
		return "", errors.New("VAULT_TOKEN or VAULT_TOKEN_FILE must be set")
	}
}

// Client reads secrets from a Vault KV version 2 secrets engine.
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient builds a Client from Config. Server identity is verified using
// the system trust store, plus CACertPath when set; TLS verification is
// never disabled.
func NewClient(config Config) (*Client, error) {
	if config.Address == "" {
		return nil, errors.New("vault config requires an address")
	}
	if config.Token == "" {
		return nil, errors.New("vault config requires a token")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if config.CACertPath != "" {
		pem, err := os.ReadFile(config.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("read VAULT_CACERT: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("VAULT_CACERT does not contain a valid PEM certificate")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
		},
	}, nil
}

// Resolve reads one field from the KV version 2 secret at path, within the
// configured mount. It returns ErrUnavailable if the secret, its latest
// version, or the field is absent.
func (c *Client) Resolve(ctx context.Context, path, field string) ([]byte, error) {
	if path == "" || field == "" {
		return nil, errors.New("vault credential reference requires a secret path and field")
	}

	status, body, err := c.do(ctx, http.MethodGet, "/v1/"+c.config.Mount+"/data/"+strings.TrimPrefix(path, "/"), nil)
	if err != nil {
		return nil, fmt.Errorf("vault request for secret %q: %w", path, err)
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: vault secret %q was not found", ErrUnavailable, path)
	}

	var decoded struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Destroyed    bool   `json:"destroyed"`
				DeletionTime string `json:"deletion_time"`
			} `json:"metadata"`
		} `json:"data"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode vault response for secret %q: %w", path, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("vault request for secret %q failed with status %d: %s", path, status, summarizeVaultErrors(decoded.Errors))
	}
	if decoded.Data.Metadata.Destroyed || decoded.Data.Metadata.DeletionTime != "" {
		return nil, fmt.Errorf("%w: vault secret %q latest version is destroyed or deleted", ErrUnavailable, path)
	}
	if len(decoded.Data.Data) == 0 {
		return nil, fmt.Errorf("%w: vault secret %q has no data", ErrUnavailable, path)
	}

	raw, ok := decoded.Data.Data[field]
	if !ok {
		return nil, fmt.Errorf("%w: vault secret %q has no field %q", ErrUnavailable, path, field)
	}
	value, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("vault secret %q field %q is not a string", path, field)
	}
	if value == "" {
		return nil, fmt.Errorf("%w: vault secret %q field %q is empty", ErrUnavailable, path, field)
	}
	return []byte(value), nil
}

// LookupSelf reports the remaining time-to-live and renewability of the
// configured token.
func (c *Client) LookupSelf(ctx context.Context) (ttl time.Duration, renewable bool, err error) {
	status, body, err := c.do(ctx, http.MethodGet, "/v1/auth/token/lookup-self", nil)
	if err != nil {
		return 0, false, fmt.Errorf("vault token lookup: %w", err)
	}

	var decoded struct {
		Data struct {
			TTL       int  `json:"ttl"`
			Renewable bool `json:"renewable"`
		} `json:"data"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return 0, false, fmt.Errorf("decode vault token lookup response: %w", err)
	}
	if status != http.StatusOK {
		return 0, false, fmt.Errorf("vault token lookup failed with status %d: %s", status, summarizeVaultErrors(decoded.Errors))
	}
	return time.Duration(decoded.Data.TTL) * time.Second, decoded.Data.Renewable, nil
}

// RenewSelf extends the configured token's lease and returns the new
// time-to-live. Long-running consumers that hold a Client beyond a single
// resolve should call this before the current lease expires; Topo does not
// invoke it automatically today because credential values are currently
// resolved once at process startup rather than kept alive across a lease.
func (c *Client) RenewSelf(ctx context.Context) (time.Duration, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/v1/auth/token/renew-self", strings.NewReader("{}"))
	if err != nil {
		return 0, fmt.Errorf("vault token renewal: %w", err)
	}

	var decoded struct {
		Auth *struct {
			LeaseDuration int  `json:"lease_duration"`
			Renewable     bool `json:"renewable"`
		} `json:"auth"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return 0, fmt.Errorf("decode vault token renewal response: %w", err)
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("vault token renewal failed with status %d: %s", status, summarizeVaultErrors(decoded.Errors))
	}
	if decoded.Auth == nil {
		return 0, errors.New("vault token renewal response has no auth data")
	}
	if !decoded.Auth.Renewable {
		return 0, errors.New("vault token is not renewable")
	}
	return time.Duration(decoded.Auth.LeaseDuration) * time.Second, nil
}

func (c *Client) do(ctx context.Context, method, requestPath string, requestBody io.Reader) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.config.Address+requestPath, requestBody)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("X-Vault-Token", c.config.Token)
	if c.config.Namespace != "" {
		request.Header.Set("X-Vault-Namespace", c.config.Namespace)
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return 0, nil, errors.New("response exceeds 1048576 bytes")
	}
	return response.StatusCode, body, nil
}

func summarizeVaultErrors(messages []string) string {
	if len(messages) == 0 {
		return "no error detail"
	}
	joined := strings.Join(messages, "; ")
	const maxMessageBytes = 200
	if len(joined) > maxMessageBytes {
		joined = joined[:maxMessageBytes] + "..."
	}
	return joined
}
