package servicenow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestPublishBatchRejectsIREHasErrorResponseWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"items":[{"hasError":true}]}}`))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	result, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{{Assets: []model.Asset{{Type: model.AssetHost, NativeID: "h1"}}}})
	if err == nil || result.Rejected != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() {
		t.Fatalf("error = %#v, want non-retryable PublishError", err)
	}
}

func TestPublishBatchClassifiesServiceUnavailableAsRetryable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{{Assets: []model.Asset{{Type: model.AssetHost, NativeID: "h1"}}}})
	var publishError *PublishError
	if !errors.As(err, &publishError) || !publishError.Retryable() {
		t.Fatalf("error = %#v, want retryable PublishError", err)
	}
}

func TestPreviewProducesIREIdentityAndRelationship(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	e := model.ObservationEnvelope{ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Assets: []model.Asset{{Type: model.AssetHost, NativeID: "h1", Name: "host"}, {Type: model.AssetNetworkInterface, NativeID: "n1", Name: "eth0"}}, Relationships: []model.Relationship{{Type: "host_has_interface", FromNativeID: "h1", ToNativeID: "n1"}}}
	v, err := p.Preview(context.Background(), []model.ObservationEnvelope{e})
	if err != nil {
		t.Fatal(err)
	}
	payload := v.(IREPayload)
	if len(payload.Items) != 2 || len(payload.Relations) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload.Items[0].SourceInfo.SourceNativeKey != "h1" {
		t.Fatal("source native key missing")
	}
}

func TestMapPayloadDeduplicatesRepeatedAssetsAcrossEnvelopes(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	first := model.ObservationEnvelope{
		ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Assets:     []model.Asset{{Type: model.AssetHost, NativeID: "h1", Name: "host-old-name"}},
	}
	second := model.ObservationEnvelope{
		ObservedAt: time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC),
		Assets:     []model.Asset{{Type: model.AssetHost, NativeID: "h1", Name: "host-new-name"}},
	}
	payload := p.mapPayload([]model.ObservationEnvelope{first, second})
	if len(payload.Items) != 1 {
		t.Fatalf("expected one deduplicated item for a repeated source_native_key, got %d", len(payload.Items))
	}
	if got := payload.Items[0].Values["name"]; got != "host-new-name" {
		t.Fatalf("expected the most recent observation to win, got name = %v", got)
	}
}

func TestMapPayloadDeduplicatesRepeatedRelationships(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	rel := model.Relationship{Type: "host_has_interface", FromNativeID: "h1", ToNativeID: "n1"}
	e := model.ObservationEnvelope{
		Assets:        []model.Asset{{Type: model.AssetHost, NativeID: "h1"}, {Type: model.AssetNetworkInterface, NativeID: "n1"}},
		Relationships: []model.Relationship{rel, rel},
	}
	payload := p.mapPayload([]model.ObservationEnvelope{e, e})
	if len(payload.Relations) != 1 {
		t.Fatalf("expected one deduplicated relationship, got %d", len(payload.Relations))
	}
}

func TestPublishBatchCapturesResponseBodyDiagnostics(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/now/identifyreconcile/enhanced" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization header = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	e := model.ObservationEnvelope{Assets: []model.Asset{{Type: model.AssetHost, NativeID: "h1"}}}
	result, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{e})
	if err != nil {
		t.Fatal(err)
	}
	if result.Published != 1 {
		t.Fatalf("published = %d", result.Published)
	}
	response, _ := result.Diagnostics["response"].(string)
	if !strings.Contains(response, "ok") {
		t.Fatalf("diagnostics response = %v, want it to contain the ServiceNow response body", result.Diagnostics["response"])
	}
}

func TestValidateRequiresHTTPS(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "http://example.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	if err := p.Validate(context.Background()); err == nil {
		t.Fatal("expected HTTPS validation error")
	}
}

// TestValidateRejectsUserinfoPathQueryFragment matches the stricter
// instance-URL contract already used by pkg/credentialref/vault: the code
// itself appends the fixed IRE API path, so InstanceURL must be bare
// scheme+host. TSR-2026-004.
func TestValidateRejectsUserinfoPathQueryFragment(t *testing.T) {
	for _, instanceURL := range []string{
		"https://user:pass@example.service-now.com",
		"https://example.service-now.com/extra/path",
		"https://example.service-now.com/?query=1",
		"https://example.service-now.com/#frag",
	} {
		p := Publisher{Config: Config{InstanceURL: instanceURL, DiscoverySource: "Nischoy Topo", DryRun: true}}
		if err := p.Validate(context.Background()); err == nil {
			t.Errorf("InstanceURL %q: expected validation error, got nil", instanceURL)
		}
	}
}

// TestPublishBatchDoesNotFollowRedirect proves defaultHTTPClient — what
// PublishBatch actually uses whenever Config.HTTPClient is nil — never
// follows a redirect, so the ServiceNow bearer token is never replayed
// against a destination the operator did not configure. TSR-2026-004.
func TestPublishBatchDoesNotFollowRedirect(t *testing.T) {
	var redirectTargetHit bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirected":
			redirectTargetHit = true
			w.WriteHeader(http.StatusOK)
		default:
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("origin should still receive the configured bearer token")
			}
			http.Redirect(w, r, server.URL+"/redirected", http.StatusFound)
		}
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/now/identifyreconcile/enhanced", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := defaultHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected the unfollowed 302 itself, got %s", resp.Status)
	}
	if redirectTargetHit {
		t.Fatal("redirect target must never be contacted")
	}
}
