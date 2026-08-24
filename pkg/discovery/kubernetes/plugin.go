// Package kubernetes discovers a Kubernetes cluster's own node and pod
// inventory over the real Kubernetes API, using only read-only list
// operations — it never issues a create, update, patch, or delete against
// any cluster object.
package kubernetes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// maxObjects bounds each of the node and pod listings so a runaway or
// hostile API server cannot force unbounded memory use, matching the
// VMware managed-object cap and the SNMP interface-table walk cap. This
// slice does not implement chunked pagination beyond that single bound —
// a cluster with more nodes or pods than the cap is reported as a
// collection error rather than silently truncated or paginated across
// multiple requests.
const maxObjects = 100000

type Config struct {
	BearerToken string
	// LabMode permits skipping API server TLS certificate verification and
	// relaxes the HTTPS-only requirement, restricted to loopback Topo Lab
	// targets. Production targets always verify the presented certificate.
	LabMode     bool
	Concurrency int
	// OperationTimeout bounds every individual HTTP request the client
	// issues, including the connectivity check. A bearer-token REST client
	// has no separate login handshake the way VMware's SOAP session or
	// WinRM's NTLM negotiation do, so there is no distinct connect phase to
	// give its own timeout.
	OperationTimeout time.Duration
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "kubernetes-cluster",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetKubernetesObject},
		RequiredPermissions: []string{
			"read-only Kubernetes RBAC access to list Nodes and Pods cluster-wide (the built-in view ClusterRole is sufficient; no create, update, patch, or delete verb is ever used)",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one Kubernetes API server target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("Kubernetes concurrency must be between 0 (default) and 1024")
	}
	if p.Config.OperationTimeout < 0 {
		return errors.New("Kubernetes operation timeout cannot be negative")
	}
	for key := range r.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("Kubernetes secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.BearerToken == "" {
		return errors.New("Kubernetes bearer token is required")
	}
	if len(p.Config.BearerToken) > 8192 {
		return errors.New("Kubernetes bearer token exceeds 8192 bytes")
	}
	if strings.ContainsAny(p.Config.BearerToken, "\x00\r\n") {
		return errors.New("Kubernetes bearer token contains a control character")
	}
	for _, raw := range r.Targets {
		if _, err := validateTarget(raw, p.Config.LabMode); err != nil {
			return err
		}
	}
	return nil
}

func (p Plugin) CheckConnectivity(ctx context.Context, r discovery.Request) error {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return err
	}
	client, err := p.dial(r.Targets[0])
	if err != nil {
		return err
	}
	_, err = client.Discovery().ServerVersion()
	return err
}

func (p Plugin) Discover(ctx context.Context, r discovery.Request) (model.ObservationEnvelope, error) {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return model.ObservationEnvelope{}, err
	}
	now := time.Now().UTC()
	obs := model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: observationID(),
		SiteID:        valueOr(r.SiteID, "default"),
		CollectorID:   valueOr(r.CollectorID, "kubernetes-relay"),
		Plugin:        "kubernetes-cluster",
		JobID:         r.JobID,
		ObservedAt:    now,
	}
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 8
	}
	jobs := make(chan string)
	var workers sync.WaitGroup
	var mu sync.Mutex
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range jobs {
				inventory, collectionErrors := p.discoverTarget(ctx, target)
				mu.Lock()
				if inventory != nil {
					assets, relationships := inventory.Assets(now)
					obs.Assets = append(obs.Assets, assets...)
					obs.Relationships = append(obs.Relationships, relationships...)
				}
				obs.Errors = append(obs.Errors, collectionErrors...)
				mu.Unlock()
			}
		}()
	}
	for _, target := range r.Targets {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return model.ObservationEnvelope{}, ctx.Err()
		case jobs <- target:
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return model.ObservationEnvelope{}, err
	}
	return obs, nil
}

func (p Plugin) discoverTarget(ctx context.Context, rawTarget string) (*Inventory, []model.CollectionError) {
	client, err := p.dial(rawTarget)
	if err != nil {
		return nil, []model.CollectionError{{Code: "kubernetes_connect", Message: rawTarget + ": " + err.Error(), Retryable: retryable(err)}}
	}

	timeout := p.operationTimeout()
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	nodeList, err := client.CoreV1().Nodes().List(opCtx, metav1.ListOptions{Limit: maxObjects + 1})
	if err != nil {
		return nil, []model.CollectionError{{Code: "kubernetes_operation", Message: rawTarget + ": list nodes: " + err.Error(), Retryable: retryable(err)}}
	}
	if len(nodeList.Items) > maxObjects {
		return nil, []model.CollectionError{{Code: "kubernetes_operation", Message: fmt.Sprintf("%s: node inventory exceeded %d objects", rawTarget, maxObjects)}}
	}
	nodes, nodesByName := mapNodes(nodeList.Items, rawTarget)

	var collectionErrors []model.CollectionError
	var pods []PodInventory
	podList, err := client.CoreV1().Pods("").List(opCtx, metav1.ListOptions{Limit: maxObjects + 1})
	if err != nil {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "kubernetes_partial", Message: rawTarget + ": list pods: " + err.Error(), Retryable: retryable(err)})
	} else if len(podList.Items) > maxObjects {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "kubernetes_partial", Message: fmt.Sprintf("%s: pod inventory exceeded %d objects", rawTarget, maxObjects)})
	} else {
		pods = mapPods(podList.Items, rawTarget, nodesByName)
	}

	return &Inventory{Nodes: nodes, Pods: pods}, collectionErrors
}

func (p Plugin) dial(rawTarget string) (*kubernetes.Clientset, error) {
	target, err := validateTarget(rawTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	cfg := &rest.Config{
		Host:        target.String(),
		BearerToken: p.Config.BearerToken,
	}
	if p.Config.LabMode {
		cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	}
	timeout := p.operationTimeout()
	cfg.Timeout = timeout
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (p Plugin) operationTimeout() time.Duration {
	if p.Config.OperationTimeout > 0 {
		return p.Config.OperationTimeout
	}
	return 30 * time.Second
}

func validateTarget(raw string, labMode bool) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errors.New("Kubernetes API server target exceeds 2048 bytes")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Kubernetes API server target %q", raw)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("Kubernetes API server target %q must not contain credentials", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Kubernetes API server target %q must not contain a query or fragment", raw)
	}
	if labMode {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("Topo Lab Kubernetes target %q must use HTTP or HTTPS", raw)
		}
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("Topo Lab Kubernetes target %q must be loopback", raw)
		}
	} else if parsed.Scheme != "https" {
		return nil, fmt.Errorf("Kubernetes API server target %q must use HTTPS", raw)
	}
	return parsed, nil
}

func retryable(err error) bool {
	var netError net.Error
	if errors.As(err, &netError) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(err.Error(), "timeout")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func observationID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
