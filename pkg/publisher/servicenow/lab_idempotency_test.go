package servicenow

// This file validates that Topo's own outbound behavior is duplicate-CI
// safe: repeated scans of the same Topo Lab estate must map to the same set
// of (className, source_native_key) pairs, and resubmitting them must send
// an idempotent, well-formed request. It does not validate ServiceNow's own
// identification/reconciliation logic or response schema, which are
// proprietary and unverified here without a real instance; see
// docs/servicenow.md.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestMapPayloadIsIdempotentAcrossRepeatedLabScans(t *testing.T) {
	scenario := lab.DefaultScenario(200, 30, 7)
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(lab.NewServer(estate).Handler())
	defer server.Close()
	options := lab.RunOptions{BaseURL: server.URL, Concurrency: 32, RequestTimeout: 2 * time.Second}

	first, err := lab.Discover(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lab.Discover(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Discovered != 200 || second.Discovered != 200 {
		t.Fatalf("unexpected discovery counts: first=%d second=%d", first.Discovered, second.Discovered)
	}

	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	firstPayload, err := p.mapPayload([]model.ObservationEnvelope{first.Observation})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := p.mapPayload([]model.ObservationEnvelope{second.Observation})
	if err != nil {
		t.Fatal(err)
	}

	firstKeys := classBySourceKey(firstPayload)
	secondKeys := classBySourceKey(secondPayload)
	if len(firstKeys) != len(secondKeys) {
		t.Fatalf("source_native_key count changed across scans: scan 1 = %d, scan 2 = %d", len(firstKeys), len(secondKeys))
	}
	for key, class := range firstKeys {
		gotClass, ok := secondKeys[key]
		if !ok {
			t.Fatalf("source_native_key %q was present in scan 1 but missing from scan 2; ServiceNow's IRE would treat this as the CI having disappeared rather than being reconciled", key)
		}
		if gotClass != class {
			t.Fatalf("source_native_key %q changed className %q -> %q across scans; ServiceNow's IRE identifies a CI by (source_native_key, className) together", key, class, gotClass)
		}
	}
}

func TestPublishBatchSendsIdempotentRequestsAcrossRepeatedLabScans(t *testing.T) {
	scenario := lab.DefaultScenario(50, 30, 11)
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	labServer := httptest.NewServer(lab.NewServer(estate).Handler())
	defer labServer.Close()
	options := lab.RunOptions{BaseURL: labServer.URL, Concurrency: 16, RequestTimeout: 2 * time.Second}

	first, err := lab.Discover(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := lab.Discover(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}

	var requestSourceKeys []map[string]string
	ireServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/now/identifyreconcile/enhanced" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var payload IREPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requestSourceKeys = append(requestSourceKeys, classBySourceKey(payload))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ireServer.Close()

	p := Publisher{Config: Config{InstanceURL: ireServer.URL, Token: "test-token", DiscoverySource: "Nischoy Topo", HTTPClient: ireServer.Client()}}
	if _, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{first.Observation}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{second.Observation}); err != nil {
		t.Fatal(err)
	}

	if len(requestSourceKeys) != 2 {
		t.Fatalf("expected 2 requests to the fake IRE endpoint, got %d", len(requestSourceKeys))
	}
	if len(requestSourceKeys[0]) == 0 {
		t.Fatal("first request carried no items")
	}
	if len(requestSourceKeys[0]) != len(requestSourceKeys[1]) {
		t.Fatalf("request source_native_key count changed across scans: %d vs %d", len(requestSourceKeys[0]), len(requestSourceKeys[1]))
	}
	for key, class := range requestSourceKeys[0] {
		if got := requestSourceKeys[1][key]; got != class {
			t.Fatalf("source_native_key %q sent with className %q on scan 1 but %q on scan 2", key, class, got)
		}
	}
}

func classBySourceKey(payload IREPayload) map[string]string {
	out := make(map[string]string, len(payload.Items))
	for _, item := range payload.Items {
		out[item.SourceInfo.SourceNativeKey] = item.ClassName
	}
	return out
}
