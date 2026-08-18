// Package credentialref resolves bounded credential references without
// accepting credential values as command-line arguments.
package credentialref

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Nischoy-ai/topo/pkg/credentialref/kubernetes"
	"github.com/Nischoy-ai/topo/pkg/credentialref/vault"
)

const (
	maxReferenceBytes  = 4 << 10
	maxCredentialBytes = 1 << 20

	// vaultResolveTimeout bounds a vault: reference resolution, including
	// connection and read time. It is a hard backstop outside the Vault
	// client's own per-request timeout.
	vaultResolveTimeout = 20 * time.Second

	// kubernetesResolveTimeout bounds a k8s: reference resolution, including
	// connection and read time. It is a hard backstop outside the
	// Kubernetes client's own per-request timeout.
	kubernetesResolveTimeout = 20 * time.Second
)

// ErrUnavailable indicates that a referenced credential is not present.
// Callers may ignore this error only for an explicitly optional credential.
var ErrUnavailable = errors.New("credential reference is unavailable")

// Resolve reads an env: or file: credential reference. It returns the exact
// credential bytes and never includes them in an error.
func Resolve(reference string) ([]byte, error) {
	if reference == "" {
		return nil, errors.New("credential reference is empty")
	}
	if len(reference) > maxReferenceBytes {
		return nil, errors.New("credential reference exceeds 4096 bytes")
	}
	kind, location, ok := strings.Cut(reference, ":")
	if !ok || location == "" {
		return nil, errors.New("credential reference must use env:<name> or file:<absolute-path>")
	}

	switch kind {
	case "env":
		return resolveEnvironment(location)
	case "file":
		return resolveFile(location)
	case "vault":
		return resolveVault(location)
	case "k8s":
		return resolveKubernetes(location)
	default:
		return nil, fmt.Errorf("unsupported credential reference provider %q", kind)
	}
}

func resolveEnvironment(name string) ([]byte, error) {
	if !validEnvironmentName(name) {
		return nil, errors.New("environment credential reference has an invalid variable name")
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return nil, fmt.Errorf("%w: environment variable %q is not set", ErrUnavailable, name)
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential from environment variable %q exceeds 1048576 bytes", name)
	}
	return []byte(value), nil
}

func resolveFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("file credential reference must use an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: credential file %q does not exist", ErrUnavailable, path)
		}
		return nil, fmt.Errorf("inspect credential file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credential file %q is not a regular file", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open credential file %q: %w", path, err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect open credential file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credential file %q is not a regular file", path)
	}

	value, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read credential file %q: %w", path, err)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("credential file %q is empty", path)
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential file %q exceeds 1048576 bytes", path)
	}
	return value, nil
}

// resolveVault reads a vault:<path>#<field> reference from a HashiCorp Vault
// KV version 2 secrets engine. Connection settings, including the token,
// come from the standard Vault environment variables so a token is never
// accepted as part of the reference or as a CLI argument.
func resolveVault(location string) ([]byte, error) {
	path, field, ok := strings.Cut(location, "#")
	if !ok || path == "" || field == "" {
		return nil, errors.New("vault credential reference must use vault:<path>#<field>")
	}

	config, err := vault.ConfigFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("vault credential reference: %w", err)
	}
	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("vault credential reference: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), vaultResolveTimeout)
	defer cancel()
	value, err := client.Resolve(ctx, path, field)
	if err != nil {
		if errors.Is(err, vault.ErrUnavailable) {
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, err.Error())
		}
		return nil, err
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential from vault secret %q field %q exceeds 1048576 bytes", path, field)
	}
	return value, nil
}

// resolveKubernetes reads a k8s:[<namespace>/]<secret-name>#<field>
// reference from the Kubernetes API server. Connection settings, including
// the bearer token, come from the pod's own service account so a token is
// never accepted as part of the reference or as a CLI argument; RBAC on
// that service account, not this function, decides which secrets it may
// read.
func resolveKubernetes(location string) ([]byte, error) {
	pathPart, field, ok := strings.Cut(location, "#")
	if !ok || pathPart == "" || field == "" {
		return nil, errors.New("kubernetes credential reference must use k8s:[<namespace>/]<secret-name>#<field>")
	}
	namespace, name := "", pathPart
	if ns, rest, found := strings.Cut(pathPart, "/"); found {
		namespace, name = ns, rest
	}
	if name == "" {
		return nil, errors.New("kubernetes credential reference must use k8s:[<namespace>/]<secret-name>#<field>")
	}

	config, err := kubernetes.ConfigFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("kubernetes credential reference: %w", err)
	}
	client, err := kubernetes.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("kubernetes credential reference: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), kubernetesResolveTimeout)
	defer cancel()
	value, err := client.Resolve(ctx, namespace, name, field)
	if err != nil {
		if errors.Is(err, kubernetes.ErrUnavailable) {
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, err.Error())
		}
		return nil, err
	}
	if len(value) > maxCredentialBytes {
		return nil, fmt.Errorf("credential from kubernetes secret %q field %q exceeds 1048576 bytes", name, field)
	}
	return value, nil
}

func validEnvironmentName(name string) bool {
	for index, character := range name {
		if character > unicode.MaxASCII {
			return false
		}
		if index == 0 {
			if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if character != '_' && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return name != ""
}
