package servicenow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPublishBatchRejectsIREHasErrorResponseWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"items":[{"hasError":true}]}}`))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	result, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	if err == nil || result.Rejected != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() {
		t.Fatalf("error = %#v, want non-retryable PublishError", err)
	}
}

func TestQueryBatchUsesNonCommittingEndpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/now/identifyreconcile/queryEnhanced" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"hasError":false,"hasWarning":false}}`))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	result, err := p.QueryBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	if err != nil {
		t.Fatal(err)
	}
	if result.Destination != "servicenow-ire-query" || result.Published != 0 || result.Diagnostics["evaluated"] != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestQueryBatchRejectsWarningWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"hasError":false,"hasWarning":true}}`))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.QueryBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() || !strings.Contains(err.Error(), "hasWarning=true") {
		t.Fatalf("error = %#v, want non-retryable warning", err)
	}
}

func TestPublishBatchClassifiesServiceUnavailableAsRetryable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || !publishError.Retryable() {
		t.Fatalf("error = %#v, want retryable PublishError", err)
	}
}

func TestPublishBatchClassifiesTransportFailureAsRetryable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network unavailable")
	})}
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: client}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || !publishError.Retryable() {
		t.Fatalf("error = %#v, want retryable PublishError", err)
	}
}

func TestPublishBatchHonorsCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(ctx, []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestPublishBatchClassifiesTooManyRequestsAsRetryable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || !publishError.Retryable() {
		t.Fatalf("error = %#v, want retryable PublishError", err)
	}
}

func TestPublishBatchDoesNotRetryClientFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad input", http.StatusBadRequest)
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() {
		t.Fatalf("error = %#v, want non-retryable PublishError", err)
	}
}

func TestPublishBatchRejectsOversizedResponseWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("error = %#v, want non-retryable oversized-response PublishError", err)
	}
}

func TestPublishBatchRejectsMalformedSuccessResponseWithoutRetry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(t.Context(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("error = %#v, want non-retryable malformed-response failure", err)
	}
}

func TestPreviewProducesIREIdentityAndRelationship(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	e := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1", Name: "host"}, model.Asset{Type: model.AssetNetworkInterface, NativeID: "n1", Name: "eth0"})
	e.ObservedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e.Relationships = []model.Relationship{{Type: "host_has_interface", FromNativeID: "h1", ToNativeID: "n1"}}
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
	first := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1", Name: "host-old-name"})
	first.ObservedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	second := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1", Name: "host-new-name"})
	second.ObservedAt = time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)
	payload, err := p.mapPayload([]model.ObservationEnvelope{first, second})
	if err != nil {
		t.Fatal(err)
	}
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
	e := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"}, model.Asset{Type: model.AssetNetworkInterface, NativeID: "n1"})
	e.Relationships = []model.Relationship{rel, rel}
	payload, err := p.mapPayload([]model.ObservationEnvelope{e, e})
	if err != nil {
		t.Fatal(err)
	}
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
	e := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})
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

func testEnvelope(assets ...model.Asset) model.ObservationEnvelope {
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: "observation-1",
		SiteID:        "site-1",
		CollectorID:   "collector-1",
		Plugin:        "test",
		ObservedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Assets:        assets,
	}
}

func TestValidateRequiresHTTPS(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "http://example.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	if err := p.Validate(context.Background()); err == nil {
		t.Fatal("expected HTTPS validation error")
	}
}

func TestValidateRejectsUnsafeDiscoverySourceAndToken(t *testing.T) {
	for _, config := range []Config{
		{InstanceURL: "https://example.service-now.com", DiscoverySource: " leading", DryRun: true},
		{InstanceURL: "https://example.service-now.com", DiscoverySource: "line\nbreak", DryRun: true},
		{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", Token: "token with spaces"},
	} {
		if err := (Publisher{Config: config}).Validate(context.Background()); err == nil {
			t.Fatalf("Validate accepted %#v", config)
		}
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

// TestPublishBatchDoesNotFollowRedirect proves the publisher overrides even a
// caller-supplied client's redirect policy, so the ServiceNow bearer token is
// never replayed against a destination the operator did not configure.
// TSR-2026-004.
func TestPublishBatchDoesNotFollowRedirect(t *testing.T) {
	var redirectTargetHit bool
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	p := Publisher{Config: Config{InstanceURL: server.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	_, err := p.PublishBatch(t.Context(), []model.ObservationEnvelope{testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "h1"})})
	var publishError *PublishError
	if !errors.As(err, &publishError) || publishError.Retryable() || !strings.Contains(err.Error(), "302 Found") {
		t.Fatalf("error = %#v, want the unfollowed redirect as a non-retryable failure", err)
	}
	if redirectTargetHit {
		t.Fatal("redirect target must never be contacted")
	}
}
