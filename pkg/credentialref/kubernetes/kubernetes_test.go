package kubernetes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Config) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "kubernetes-test-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return server, Config{APIServer: server.URL, DefaultNamespace: "topo", TokenPath: tokenPath, CACertPath: caPath}
}

func TestClientResolveReadsField(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/topo/secrets/ssh-credentials" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]string{"password": base64.StdEncoding.EncodeToString([]byte("hunter2"))},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "hunter2" {
		t.Fatalf("value = %q", value)
	}
}

func TestClientResolveUsesExplicitNamespace(t *testing.T) {
	var gotPath string
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]string{"password": base64.StdEncoding.EncodeToString([]byte("value"))},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), "other-namespace", "ssh-credentials", "password"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/namespaces/other-namespace/secrets/ssh-credentials" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestClientResolveMissingSecretIsUnavailable(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"message": "secrets \"missing\" not found", "reason": "NotFound",
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "missing", "password")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClientResolveMissingFieldIsUnavailable(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]string{"other": base64.StdEncoding.EncodeToString([]byte("value"))},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClientResolvePermissionDeniedDoesNotExposeSecret(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"message": "secrets \"ssh-credentials\" is forbidden", "reason": "Forbidden",
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if err == nil || !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error exposed credential: %v", err)
	}
}

func TestClientResolveRejectsInvalidBase64(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]string{"password": "not-valid-base64!!"},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientResolveNoNamespaceIsRejected(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not be sent without a namespace")
	})
	defer server.Close()
	config.DefaultNamespace = ""

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientResolveBoundsResponseSize(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "", "ssh-credentials", "password")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientResolveHonorsContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	})
	defer func() {
		close(blocked)
		server.Close()
	}()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = client.Resolve(ctx, "", "ssh-credentials", "password")
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(destination.Close)

	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), "", "ssh-credentials", "password"); err == nil {
		t.Fatal("expected redirected Kubernetes response to fail")
	}
	if redirected.Load() {
		t.Fatal("Kubernetes client followed a redirect and risked forwarding its token")
	}
}

func TestClientResolveRereadsTokenEveryRequest(t *testing.T) {
	var gotTokens []string
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotTokens = append(gotTokens, r.Header.Get("Authorization"))
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]string{"password": base64.StdEncoding.EncodeToString([]byte("value"))},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), "", "ssh-credentials", "password"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.TokenPath, []byte("rotated-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), "", "ssh-credentials", "password"); err != nil {
		t.Fatal(err)
	}
	if len(gotTokens) != 2 || gotTokens[0] != "Bearer test-token" || gotTokens[1] != "Bearer rotated-token" {
		t.Fatalf("tokens = %v", gotTokens)
	}
}

func TestConfigFromEnvironmentRequiresAPIServer(t *testing.T) {
	clearKubernetesEnvironment(t)
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "KUBERNETES_SERVICE_HOST") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigFromEnvironmentRejectsPlainHTTP(t *testing.T) {
	clearKubernetesEnvironment(t)
	t.Setenv("TOPO_KUBERNETES_API_SERVER", "http://k8s.example.test")
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewClientRejectsPlainHTTP(t *testing.T) {
	if _, err := NewClient(Config{APIServer: "http://k8s.example.test", TokenPath: defaultTokenPath}); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigFromEnvironmentBuildsAPIServerFromHostPort(t *testing.T) {
	clearKubernetesEnvironment(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.APIServer != "https://10.0.0.1:443" {
		t.Fatalf("api server = %q", config.APIServer)
	}
	if config.TokenPath != defaultTokenPath || config.CACertPath != defaultCACertPath {
		t.Fatalf("token path = %q, ca path = %q", config.TokenPath, config.CACertPath)
	}
}

func TestConfigFromEnvironmentExplicitAPIServerOverrides(t *testing.T) {
	clearKubernetesEnvironment(t)
	t.Setenv("TOPO_KUBERNETES_API_SERVER", "https://k8s.example.test:6443")
	t.Setenv("TOPO_KUBERNETES_NAMESPACE", "topo-system")

	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.APIServer != "https://k8s.example.test:6443" {
		t.Fatalf("api server = %q", config.APIServer)
	}
	if config.CACertPath != "" {
		t.Fatalf("ca cert path = %q, want empty for an explicit API server override", config.CACertPath)
	}
	if config.DefaultNamespace != "topo-system" {
		t.Fatalf("namespace = %q", config.DefaultNamespace)
	}
}

func TestNewClientRejectsInvalidCACert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewClient(Config{APIServer: "https://k8s.example.test", TokenPath: defaultTokenPath, CACertPath: path})
	if err == nil || !strings.Contains(err.Error(), "does not contain a valid PEM certificate") {
		t.Fatalf("error = %v", err)
	}
}

func clearKubernetesEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT",
		"TOPO_KUBERNETES_API_SERVER", "TOPO_KUBERNETES_TOKEN_FILE",
		"TOPO_KUBERNETES_CA_FILE", "TOPO_KUBERNETES_NAMESPACE",
	} {
		t.Setenv(name, "")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
