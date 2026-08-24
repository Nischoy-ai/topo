// Package servicenow maps Nischoy Topo assets to ServiceNow IRE requests.
package servicenow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

type Config struct {
	InstanceURL     string
	Token           string
	DiscoverySource string
	DryRun          bool
	HTTPClient      *http.Client
}

type Publisher struct{ Config Config }

type IREPayload struct {
	Items     []IREItem     `json:"items"`
	Relations []IRERelation `json:"relations,omitempty"`
}
type IREItem struct {
	ClassName  string         `json:"className"`
	Values     map[string]any `json:"values"`
	SourceInfo SourceInfo     `json:"sys_object_source_info"`
}
type SourceInfo struct {
	SourceName      string `json:"source_name"`
	SourceNativeKey string `json:"source_native_key"`
}
type IRERelation struct {
	Type   string `json:"type"`
	Parent int    `json:"parent"`
	Child  int    `json:"child"`
}

func (p Publisher) Validate(context.Context) error {
	if p.Config.InstanceURL == "" {
		return errors.New("ServiceNow instance URL is required")
	}
	u, err := url.Parse(p.Config.InstanceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("ServiceNow instance URL must be an absolute HTTPS URL")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("ServiceNow instance URL must not contain credentials, a path, query parameters, or a fragment")
	}
	if p.Config.DiscoverySource == "" {
		return errors.New("ServiceNow discovery source is required")
	}
	if !p.Config.DryRun && p.Config.Token == "" {
		return errors.New("ServiceNow token is required outside dry-run mode")
	}
	return nil
}

func (p Publisher) Preview(_ context.Context, envelopes []model.ObservationEnvelope) (any, error) {
	return p.mapPayload(envelopes), nil
}

func (p Publisher) PublishBatch(ctx context.Context, envelopes []model.ObservationEnvelope) (publisher.Result, error) {
	if err := p.Validate(ctx); err != nil {
		return publisher.Result{}, err
	}
	payload := p.mapPayload(envelopes)
	if p.Config.DryRun {
		return publisher.Result{Destination: "servicenow-ire-query", Published: len(payload.Items), Diagnostics: map[string]any{"payload": payload}}, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return publisher.Result{}, err
	}
	endpoint := strings.TrimRight(p.Config.InstanceURL, "/") + "/api/now/identifyreconcile/enhanced"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return publisher.Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := p.Config.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return publisher.Result{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return publisher.Result{Destination: "servicenow-ire", Rejected: len(payload.Items)}, fmt.Errorf("ServiceNow IRE returned %s: %s", resp.Status, string(body))
	}
	// The response body is captured for operator diagnostics (for example,
	// manual reconciliation review) rather than parsed, since ServiceNow's
	// exact IRE response schema is proprietary and unverified against a real
	// instance; see docs/servicenow.md.
	return publisher.Result{Destination: "servicenow-ire", Published: len(payload.Items), Diagnostics: map[string]any{"status": resp.StatusCode, "response": string(body)}}, nil
}

// defaultHTTPClient is used whenever Config.HTTPClient is nil. It never
// follows a redirect: the Authorization header set above must not be
// replayed against a destination the operator did not configure.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// mapPayload builds the IRE request payload. Each source_native_key appears
// at most once, and each (type, from, to) relationship appears at most once,
// even across multiple input envelopes (for example, a batch of several
// buffered observations covering the same asset): ServiceNow's IRE matches
// and reconciles CIs by source_native_key, so submitting the same key twice
// in one request risks a duplicate or conflicting reconciliation rather than
// updating one CI. When an asset appears more than once, the most recent
// envelope's values win, matching store.Memory's resolved-asset semantics.
func (p Publisher) mapPayload(envelopes []model.ObservationEnvelope) IREPayload {
	out := IREPayload{}
	index := map[string]int{}
	for _, e := range envelopes {
		for _, a := range e.Assets {
			values := map[string]any{"name": a.Name, "discovery_source": p.Config.DiscoverySource, "last_discovered": e.ObservedAt.Format("2006-01-02 15:04:05")}
			for k, v := range a.Attributes {
				values[k] = v
			}
			item := IREItem{ClassName: classFor(a.Type), Values: values, SourceInfo: SourceInfo{SourceName: p.Config.DiscoverySource, SourceNativeKey: a.NativeID}}
			if pos, exists := index[a.NativeID]; exists {
				out.Items[pos] = item
				continue
			}
			index[a.NativeID] = len(out.Items)
			out.Items = append(out.Items, item)
		}
	}

	type relationKey struct{ typ, from, to string }
	seenRelations := map[relationKey]bool{}
	for _, e := range envelopes {
		for _, r := range e.Relationships {
			from, ok1 := index[r.FromNativeID]
			to, ok2 := index[r.ToNativeID]
			if !ok1 || !ok2 {
				continue
			}
			key := relationKey{r.Type, r.FromNativeID, r.ToNativeID}
			if seenRelations[key] {
				continue
			}
			seenRelations[key] = true
			out.Relations = append(out.Relations, IRERelation{Type: relationFor(r.Type), Parent: from, Child: to})
		}
	}
	return out
}

func classFor(t model.AssetType) string {
	switch t {
	case model.AssetHost:
		return "cmdb_ci_computer"
	case model.AssetNetworkInterface:
		return "cmdb_ci_network_adapter"
	case model.AssetVolume:
		return "cmdb_ci_disk"
	case model.AssetSoftware:
		return "cmdb_ci_spkg"
	case model.AssetVirtualMachine:
		return "cmdb_ci_vm_instance"
	default:
		return "cmdb_ci"
	}
}
func relationFor(t string) string {
	switch t {
	case "host_has_interface":
		return "Owns::Owned by"
	default:
		return t
	}
}
