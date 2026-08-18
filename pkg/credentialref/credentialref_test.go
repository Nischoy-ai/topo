package credentialref

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnvironment(t *testing.T) {
	t.Setenv("TOPO_TEST_CREDENTIAL", "sensitive-value\n")
	value, err := Resolve("env:TOPO_TEST_CREDENTIAL")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "sensitive-value\n" {
		t.Fatalf("credential bytes changed: %q", value)
	}
}

func TestResolveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := Resolve("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "file-secret\n" {
		t.Fatalf("credential bytes changed: %q", value)
	}
}

func TestResolveVault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/topo/ssh" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Vault-Token"); got != "test-token" {
			t.Fatalf("token header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]any{"password": "vault-secret"}},
		})
	}))
	defer server.Close()
	t.Setenv("VAULT_ADDR", server.URL)
	t.Setenv("VAULT_TOKEN", "test-token")

	value, err := Resolve("vault:topo/ssh#password")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "vault-secret" {
		t.Fatalf("credential bytes changed: %q", value)
	}
}

func TestResolveVaultUnavailableDoesNotExposeCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("VAULT_ADDR", server.URL)
	t.Setenv("VAULT_TOKEN", "test-token")

	_, err := Resolve("vault:topo/missing#password")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestResolveVaultMissingConfiguration(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	_, err := Resolve("vault:topo/ssh#password")
	if err == nil || !strings.Contains(err.Error(), "VAULT_ADDR") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		contains  string
	}{
		{name: "empty", contains: "empty"},
		{name: "value", reference: "secret", contains: "must use"},
		{name: "provider", reference: "azure:path", contains: "unsupported"},
		{name: "vault missing field", reference: "vault:secret/topo", contains: "vault:<path>#<field>"},
		{name: "environment name", reference: "env:BAD-NAME", contains: "invalid"},
		{name: "relative file", reference: "file:relative", contains: "absolute"},
		{name: "directory", reference: "file:" + t.TempDir(), contains: "regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(test.reference)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("Resolve(%q) error = %v, want containing %q", test.reference, err, test.contains)
			}
		})
	}
}

func TestResolveUnavailableDoesNotExposeCredential(t *testing.T) {
	const name = "TOPO_TEST_MISSING_CREDENTIAL"
	t.Setenv(name, "should-not-appear")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("env:" + name)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), "should-not-appear") {
		t.Fatalf("error exposed credential: %v", err)
	}
}

func TestResolveBoundsCredentialValues(t *testing.T) {
	credential := "must-not-appear-" + strings.Repeat("x", maxCredentialBytes)
	t.Setenv("TOPO_TEST_LARGE_CREDENTIAL", credential)
	if _, err := Resolve("env:TOPO_TEST_LARGE_CREDENTIAL"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized environment credential error = %v", err)
	} else if strings.Contains(err.Error(), credential[:64]) {
		t.Fatalf("error exposed credential: %v", err)
	}

	path := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(path, []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("file:" + path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized file credential error = %v", err)
	} else if strings.Contains(err.Error(), credential[:64]) {
		t.Fatalf("error exposed credential: %v", err)
	}
}
