package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Config) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, Config{Address: server.URL, Mount: "secret", Token: "test-token"}
}

func TestClientResolveReadsField(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/topo/ssh" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Vault-Token"); got != "test-token" {
			t.Fatalf("token header = %q", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"data": map[string]any{"password": "hunter2"},
			},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Resolve(context.Background(), "topo/ssh", "password")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "hunter2" {
		t.Fatalf("value = %q", value)
	}
}

func TestClientResolveSetsNamespace(t *testing.T) {
	var gotNamespace string
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotNamespace = r.Header.Get("X-Vault-Namespace")
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"data": map[string]any{"password": "value"}},
		})
	})
	defer server.Close()
	config.Namespace = "team-a"

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resolve(context.Background(), "topo/ssh", "password"); err != nil {
		t.Fatal(err)
	}
	if gotNamespace != "team-a" {
		t.Fatalf("namespace header = %q", gotNamespace)
	}
}

func TestClientResolveMissingSecretIsUnavailable(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []string{}})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "topo/missing", "password")
	if err == nil || !isUnavailable(err) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClientResolveMissingFieldIsUnavailable(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"data": map[string]any{"other": "value"}},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "topo/ssh", "password")
	if err == nil || !isUnavailable(err) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClientResolveDestroyedVersionIsUnavailable(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"data":     map[string]any{"password": "value"},
				"metadata": map[string]any{"destroyed": true},
			},
		})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "topo/ssh", "password")
	if err == nil || !isUnavailable(err) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestClientResolvePermissionDeniedDoesNotExposeSecret(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"errors": []string{"permission denied"}})
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "topo/ssh", "password")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error exposed credential: %v", err)
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
	_, err = client.Resolve(context.Background(), "topo/ssh", "password")
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
	_, err = client.Resolve(ctx, "topo/ssh", "password")
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestClientLookupAndRenewSelf(t *testing.T) {
	server, config := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/token/lookup-self":
			writeJSON(w, http.StatusOK, map[string]any{
				"data": map[string]any{"ttl": 3600, "renewable": true},
			})
		case "/v1/auth/token/renew-self":
			writeJSON(w, http.StatusOK, map[string]any{
				"auth": map[string]any{"lease_duration": 7200, "renewable": true},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
	defer server.Close()

	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	ttl, renewable, err := client.LookupSelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ttl != time.Hour || !renewable {
		t.Fatalf("ttl = %v, renewable = %v", ttl, renewable)
	}

	newTTL, err := client.RenewSelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if newTTL != 2*time.Hour {
		t.Fatalf("renewed ttl = %v", newTTL)
	}
}

func TestConfigFromEnvironmentRequiresAddress(t *testing.T) {
	clearVaultEnvironment(t)
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "VAULT_ADDR") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigFromEnvironmentRejectsConflictingToken(t *testing.T) {
	clearVaultEnvironment(t)
	t.Setenv("VAULT_ADDR", "https://vault.example.test")
	t.Setenv("VAULT_TOKEN", "a")
	t.Setenv("VAULT_TOKEN_FILE", "/run/secrets/vault-token")
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigFromEnvironmentReadsTokenFile(t *testing.T) {
	clearVaultEnvironment(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAULT_ADDR", "https://vault.example.test")
	t.Setenv("VAULT_TOKEN_FILE", path)

	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.Token != "file-token" {
		t.Fatalf("token = %q", config.Token)
	}
	if config.Mount != "secret" {
		t.Fatalf("mount = %q", config.Mount)
	}
}

func TestConfigFromEnvironmentDefaults(t *testing.T) {
	clearVaultEnvironment(t)
	t.Setenv("VAULT_ADDR", "https://vault.example.test/")
	t.Setenv("VAULT_TOKEN", "a-token")
	t.Setenv("VAULT_MOUNT", "/topo-secrets/")
	t.Setenv("VAULT_NAMESPACE", "team-a")

	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "https://vault.example.test" {
		t.Fatalf("address = %q", config.Address)
	}
	if config.Mount != "topo-secrets" {
		t.Fatalf("mount = %q", config.Mount)
	}
	if config.Namespace != "team-a" {
		t.Fatalf("namespace = %q", config.Namespace)
	}
}

func TestNewClientRejectsInvalidCACert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewClient(Config{Address: "https://vault.example.test", Token: "a-token", CACertPath: path})
	if err == nil || !strings.Contains(err.Error(), "does not contain a valid PEM certificate") {
		t.Fatalf("error = %v", err)
	}
}

func clearVaultEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"VAULT_ADDR", "VAULT_TOKEN", "VAULT_TOKEN_FILE", "VAULT_NAMESPACE", "VAULT_MOUNT", "VAULT_CACERT"} {
		t.Setenv(name, "")
	}
}

func isUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
