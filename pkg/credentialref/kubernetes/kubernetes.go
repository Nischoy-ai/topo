// Package kubernetes resolves credential values from Kubernetes Secret
// objects over the Kubernetes API server, using the pod's own service
// account for authentication and RBAC-based scoping. It never accepts a
// token as a literal CLI argument and never places resolved credential
// bytes in an error.
package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// defaultTokenPath is the projected service account token Kubernetes
	// mounts into every pod, automatically rotated by the kubelet.
	defaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	// defaultCACertPath is the cluster CA certificate Kubernetes mounts
	// alongside the token.
	defaultCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	// defaultNamespacePath is the pod's own namespace, mounted alongside the
	// token, used as the default when a reference does not specify one.
	defaultNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	// maxResponseBytes bounds the Kubernetes API response body, matching
	// the resolved-credential bound used by the other credential providers.
	maxResponseBytes = 1 << 20
	// maxTokenBytes bounds the service account token file read.
	maxTokenBytes = 64 << 10

	// requestTimeout bounds a single Kubernetes API round trip.
	requestTimeout = 15 * time.Second
)

// ErrUnavailable indicates that a referenced secret or field is not present.
// Callers may ignore this error only for an explicitly optional credential.
var ErrUnavailable = errors.New("kubernetes credential is unavailable")

// Config holds the connection settings for a Kubernetes Secret credential
// provider. Values come from the standard in-cluster service account
// environment so the adapter needs no configuration inside a pod, and its
// effective read permissions come entirely from that service account's RBAC
// grants, not from anything Topo enforces itself.
type Config struct {
	// APIServer is the Kubernetes API server base URL.
	APIServer string
	// DefaultNamespace is used when a reference does not specify one. It is
	// normally the pod's own namespace.
	DefaultNamespace string
	// TokenPath names the bearer token file, re-read on every request so
	// kubelet-rotated tokens are picked up without any renewal logic.
	TokenPath string
	// CACertPath names the cluster certificate authority trusted for the
	// API server's TLS identity. Server identity verification is always
	// enabled; Topo never disables TLS certificate verification.
	CACertPath string
}

// ConfigFromEnvironment reads Config from the standard Kubernetes in-cluster
// environment variables and service-account files: KUBERNETES_SERVICE_HOST,
// KUBERNETES_SERVICE_PORT, and the token/CA/namespace files Kubernetes
// projects into every pod. TOPO_KUBERNETES_API_SERVER,
// TOPO_KUBERNETES_TOKEN_FILE, TOPO_KUBERNETES_CA_FILE, and
// TOPO_KUBERNETES_NAMESPACE override the in-cluster defaults for testing or
// unusual deployments.
func ConfigFromEnvironment() (Config, error) {
	apiServer := strings.TrimSpace(os.Getenv("TOPO_KUBERNETES_API_SERVER"))
	inCluster := apiServer == ""
	if inCluster {
		host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
		port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
		if host == "" || port == "" {
			return Config{}, errors.New("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are not set; run inside a Kubernetes pod or set TOPO_KUBERNETES_API_SERVER")
		}
		apiServer = "https://" + net.JoinHostPort(host, port)
	}
	apiServer, err := validateHTTPSAPIServer(apiServer)
	if err != nil {
		return Config{}, err
	}

	tokenPath := strings.TrimSpace(os.Getenv("TOPO_KUBERNETES_TOKEN_FILE"))
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}
	if !filepath.IsAbs(tokenPath) {
		return Config{}, errors.New("TOPO_KUBERNETES_TOKEN_FILE must be an absolute path")
	}

	// The in-cluster CA is only assumed by default when connecting through
	// the in-cluster host/port; an explicit TOPO_KUBERNETES_API_SERVER
	// override (out-of-cluster use, testing) falls back to the system trust
	// store unless TOPO_KUBERNETES_CA_FILE is also set, matching how the
	// Vault provider treats its optional CA override.
	caCertPath := strings.TrimSpace(os.Getenv("TOPO_KUBERNETES_CA_FILE"))
	if caCertPath == "" && inCluster {
		caCertPath = defaultCACertPath
	}
	if caCertPath != "" && !filepath.IsAbs(caCertPath) {
		return Config{}, errors.New("TOPO_KUBERNETES_CA_FILE must be an absolute path")
	}

	namespace := strings.TrimSpace(os.Getenv("TOPO_KUBERNETES_NAMESPACE"))
	if namespace == "" {
		if data, err := os.ReadFile(defaultNamespacePath); err == nil {
			namespace = strings.TrimSpace(string(data))
		}
	}

	return Config{
		APIServer:        apiServer,
		DefaultNamespace: namespace,
		TokenPath:        tokenPath,
		CACertPath:       caCertPath,
	}, nil
}

// Client reads Secret objects from the Kubernetes API server.
type Client struct {
	config     Config
	httpClient *http.Client
}

// NewClient builds a Client from Config. Server identity is verified against
// CACertPath; TLS verification is never disabled, plaintext HTTP is rejected,
// and redirects are not followed.
func NewClient(config Config) (*Client, error) {
	apiServer, err := validateHTTPSAPIServer(config.APIServer)
	if err != nil {
		return nil, err
	}
	if config.TokenPath == "" {
		return nil, errors.New("kubernetes config requires a token file path")
	}
	config.APIServer = apiServer

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if config.CACertPath != "" {
		pem, err := os.ReadFile(config.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("read kubernetes CA certificate %q: %w", config.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("kubernetes CA certificate %q does not contain a valid PEM certificate", config.CACertPath)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func validateHTTPSAPIServer(apiServer string) (string, error) {
	if apiServer == "" {
		return "", errors.New("kubernetes config requires an API server address")
	}
	parsed, err := url.Parse(apiServer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("TOPO_KUBERNETES_API_SERVER must be an absolute https:// URL")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("TOPO_KUBERNETES_API_SERVER must not contain credentials, a path, query parameters, or a fragment")
	}
	return strings.TrimRight(apiServer, "/"), nil
}

// Resolve reads one field from a Kubernetes Secret. namespace may be empty
// to use the configured default namespace. It returns ErrUnavailable if the
// secret or field is absent; the caller's service account must additionally
// have RBAC "get" permission on the named Secret, which Kubernetes enforces
// independently of this client.
func (c *Client) Resolve(ctx context.Context, namespace, name, field string) ([]byte, error) {
	if namespace == "" {
		namespace = c.config.DefaultNamespace
	}
	if namespace == "" {
		return nil, errors.New("kubernetes credential reference has no namespace and no default namespace is configured")
	}
	if name == "" || field == "" {
		return nil, errors.New("kubernetes credential reference requires a secret name and field")
	}

	token, err := c.readToken()
	if err != nil {
		return nil, fmt.Errorf("read kubernetes service account token: %w", err)
	}

	requestPath := "/api/v1/namespaces/" + namespace + "/secrets/" + name
	status, body, err := c.do(ctx, requestPath, token)
	if err != nil {
		return nil, fmt.Errorf("kubernetes request for secret %q in namespace %q: %w", name, namespace, err)
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: kubernetes secret %q was not found in namespace %q", ErrUnavailable, name, namespace)
	}

	var decoded struct {
		Data    map[string]string `json:"data"`
		Message string            `json:"message"`
		Reason  string            `json:"reason"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode kubernetes response for secret %q: %w", name, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("kubernetes request for secret %q in namespace %q failed with status %d: %s", name, namespace, status, summarizeKubernetesError(decoded.Reason, decoded.Message))
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("%w: kubernetes secret %q has no data", ErrUnavailable, name)
	}

	encoded, ok := decoded.Data[field]
	if !ok {
		return nil, fmt.Errorf("%w: kubernetes secret %q has no field %q", ErrUnavailable, name, field)
	}
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("kubernetes secret %q field %q is not valid base64", name, field)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("%w: kubernetes secret %q field %q is empty", ErrUnavailable, name, field)
	}
	return value, nil
}

func (c *Client) readToken() (string, error) {
	file, err := os.Open(c.config.TokenPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	if len(data) > maxTokenBytes {
		return "", errors.New("token file exceeds 65536 bytes")
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}

func (c *Client) do(ctx context.Context, requestPath, token string) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.APIServer+requestPath, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")

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

func summarizeKubernetesError(reason, message string) string {
	switch {
	case reason != "" && message != "":
		return fmt.Sprintf("%s: %s", reason, truncate(message, 200))
	case message != "":
		return truncate(message, 200)
	case reason != "":
		return reason
	default:
		return "no error detail"
	}
}

func truncate(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	return text[:maxBytes] + "..."
}
