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
	"unicode"

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

const (
	MaxEnvelopes       = 100
	MaxItems           = 1000
	MaxRelations       = 2000
	MaxRequestBytes    = 4 << 20
	MaxResponseBytes   = 1 << 20
	MaxDiscoverySource = 100
)

// PublishError classifies an IRE failure for delivery loops. Transport
// failures, 5xx, and 429 may be retried; configuration/validation failures and
// a successful HTTP response whose JSON reports hasError=true must not be
// replayed blindly because a rejected IRE request can leave an incomplete
// identification record behind.
type PublishError struct {
	retryable bool
	err       error
}

func (e *PublishError) Error() string   { return e.err.Error() }
func (e *PublishError) Unwrap() error   { return e.err }
func (e *PublishError) Retryable() bool { return e.retryable }

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
	if len(p.Config.DiscoverySource) > MaxDiscoverySource || strings.TrimSpace(p.Config.DiscoverySource) != p.Config.DiscoverySource || strings.IndexFunc(p.Config.DiscoverySource, unicode.IsControl) >= 0 {
		return fmt.Errorf("ServiceNow discovery source must be 1 to %d bytes, have no surrounding whitespace, and contain no control characters", MaxDiscoverySource)
	}
	if !p.Config.DryRun && p.Config.Token == "" {
		return errors.New("ServiceNow token is required outside dry-run mode")
	}
	if !p.Config.DryRun && !validBearerToken(p.Config.Token) {
		return errors.New("ServiceNow token has an invalid format")
	}
	return nil
}

func (p Publisher) Preview(ctx context.Context, envelopes []model.ObservationEnvelope) (any, error) {
	if err := p.Validate(ctx); err != nil {
		return nil, err
	}
	return p.mapPayload(envelopes)
}

func (p Publisher) PublishBatch(ctx context.Context, envelopes []model.ObservationEnvelope) (publisher.Result, error) {
	if err := p.Validate(ctx); err != nil {
		return publisher.Result{}, err
	}
	payload, err := p.mapPayload(envelopes)
	if err != nil {
		return publisher.Result{}, err
	}
	if p.Config.DryRun {
		return publisher.Result{Destination: "servicenow-ire-preview", Published: len(payload.Items), Diagnostics: map[string]any{"payload": payload}}, nil
	}
	return p.sendPayload(ctx, payload, "/api/now/identifyreconcile/enhanced", "servicenow-ire", true)
}

// QueryBatch asks ServiceNow's documented queryEnhanced endpoint to evaluate
// the exact payload without committing changes. The supported CLI runs this
// authenticated server-side preflight immediately before every apply.
func (p Publisher) QueryBatch(ctx context.Context, envelopes []model.ObservationEnvelope) (publisher.Result, error) {
	if p.Config.DryRun {
		return publisher.Result{}, errors.New("ServiceNow IRE query requires authenticated non-dry-run mode")
	}
	if err := p.Validate(ctx); err != nil {
		return publisher.Result{}, err
	}
	payload, err := p.mapPayload(envelopes)
	if err != nil {
		return publisher.Result{}, err
	}
	return p.sendPayload(ctx, payload, "/api/now/identifyreconcile/queryEnhanced", "servicenow-ire-query", false)
}

func (p Publisher) sendPayload(ctx context.Context, payload IREPayload, endpointPath, destination string, commits bool) (publisher.Result, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return publisher.Result{}, err
	}
	if len(b) > MaxRequestBytes {
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, fmt.Errorf("ServiceNow IRE request exceeds %d bytes", MaxRequestBytes)
	}
	endpoint := strings.TrimRight(p.Config.InstanceURL, "/") + endpointPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return publisher.Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := boundedHTTPClient(p.Config.HTTPClient)
	resp, err := client.Do(req)
	if err != nil {
		return publisher.Result{}, &PublishError{retryable: true, err: err}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if readErr != nil {
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{err: fmt.Errorf("read ServiceNow IRE response: %w", readErr)}
	}
	if len(body) > MaxResponseBytes {
		// An apply may already have reconciled CIs. Even query responses remain
		// non-retryable here so the operator sees an ambiguous protocol result.
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{err: fmt.Errorf("ServiceNow IRE response exceeds %d bytes", MaxResponseBytes)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{retryable: retryable, err: fmt.Errorf("ServiceNow IRE returned %s: %s", resp.Status, string(body))}
	}
	if !json.Valid(body) {
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{err: errors.New("ServiceNow IRE returned a malformed JSON response")}
	}
	if responseReportsFlag(body, "hasError") {
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{err: fmt.Errorf("ServiceNow IRE response reported hasError=true: %s", string(body))}
	}
	if responseReportsFlag(body, "hasWarning") {
		return publisher.Result{Destination: destination, Rejected: len(payload.Items)}, &PublishError{err: fmt.Errorf("ServiceNow IRE response reported hasWarning=true: %s", string(body))}
	}
	// The response body is captured for operator diagnostics. Apart from the
	// documented hasError/hasWarning semantic bits above, the exact IRE
	// response schema remains an unparsed, version-independent diagnostic.
	result := publisher.Result{Destination: destination, Diagnostics: map[string]any{"status": resp.StatusCode, "response": string(body), "evaluated": len(payload.Items)}}
	if commits {
		result.Published = len(payload.Items)
	}
	return result, nil
}

// responseReportsFlag deliberately recognizes only documented semantic bits
// at any nesting depth. It does not couple Topo to the rest of ServiceNow's
// release-dependent response schema.
func responseReportsFlag(body []byte, name string) bool {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return false
	}
	var walk func(any) bool
	walk = func(value any) bool {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == name {
					if flag, ok := child.(bool); ok && flag {
						return true
					}
				}
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(decoded)
}

// defaultHTTPClient is used whenever Config.HTTPClient is nil. It never
// follows a redirect: the Authorization header set above must not be
// replayed against a destination the operator did not configure.
func boundedHTTPClient(base *http.Client) *http.Client {
	client := &http.Client{}
	if base != nil {
		*client = *base
	}
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

// mapPayload builds the IRE request payload. Each source_native_key appears
// at most once, and each (type, from, to) relationship appears at most once,
// even across multiple input envelopes (for example, a batch of several
// buffered observations covering the same asset): ServiceNow's IRE matches
// and reconciles CIs by source_native_key, so submitting the same key twice
// in one request risks a duplicate or conflicting reconciliation rather than
// updating one CI. When an asset appears more than once, the most recent
// envelope's values win, matching store.Memory's resolved-asset semantics.
func (p Publisher) mapPayload(envelopes []model.ObservationEnvelope) (IREPayload, error) {
	if err := validateEnvelopes(envelopes); err != nil {
		return IREPayload{}, err
	}
	out := IREPayload{}
	index := map[string]int{}
	for _, e := range envelopes {
		for _, a := range e.Assets {
			values := map[string]any{"name": a.Name, "discovery_source": p.Config.DiscoverySource, "last_discovered": e.ObservedAt.Format("2006-01-02 15:04:05")}
			// Only fields whose ServiceNow meaning is reviewed are copied. An
			// imported observation must not become an arbitrary CMDB-field write.
			if a.Type == model.AssetNetworkInterface {
				if mac, ok := a.Attributes["mac_address"].(string); ok && mac != "" {
					values["mac_address"] = mac
				}
			}
			className, _ := classFor(a.Type)
			item := IREItem{ClassName: className, Values: values, SourceInfo: SourceInfo{SourceName: p.Config.DiscoverySource, SourceNativeKey: a.NativeID}}
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
			relationType, _ := relationFor(r.Type)
			out.Relations = append(out.Relations, IRERelation{Type: relationType, Parent: from, Child: to})
		}
	}
	return out, nil
}

func classFor(t model.AssetType) (string, bool) {
	switch t {
	case model.AssetHost:
		return "cmdb_ci_computer", true
	case model.AssetNetworkInterface:
		return "cmdb_ci_network_adapter", true
	default:
		return "", false
	}
}
func relationFor(t string) (string, bool) {
	switch t {
	case "host_has_interface":
		return "Owns::Owned by", true
	default:
		return "", false
	}
}

func validBearerToken(token string) bool {
	if token == "" || len(token) > 64<<10 {
		return false
	}
	return strings.IndexFunc(token, unicode.IsSpace) < 0 && strings.IndexFunc(token, unicode.IsControl) < 0
}
