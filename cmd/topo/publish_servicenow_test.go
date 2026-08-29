package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
)

func TestServiceNowPublishPreviewsWithoutCredentialOrNetwork(t *testing.T) {
	var output bytes.Buffer
	err := serviceNowPublish([]string{
		"-instance", "https://example.service-now.com",
		"-token-ref", "file:/definitely/missing/topo-token",
	}, strings.NewReader(serviceNowObservationJSON(t)), &output, nil)
	if err != nil {
		t.Fatal(err)
	}
	var status serviceNowPreviewStatus
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Mode != "preview" || status.Envelopes != 1 || status.Items != 2 || status.Relations != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestServiceNowPublishApplyEndToEnd(t *testing.T) {
	const token = "test-ire-token"
	t.Setenv("TOPO_SERVICENOW_TOKEN", token)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		wantPath := "/api/now/identifyreconcile/queryEnhanced"
		if call == 2 {
			wantPath = "/api/now/identifyreconcile/enhanced"
		}
		if r.Method != http.MethodPost || r.URL.Path != wantPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		var payload servicenow.IREPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Items) != 2 || len(payload.Relations) != 1 {
			t.Errorf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"items":[{"hasError":false}]}}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := serviceNowPublish([]string{
		"-instance", server.URL,
		"-apply",
		"-max-attempts", "1",
	}, strings.NewReader(serviceNowObservationJSON(t)), &output, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var status serviceNowApplyStatus
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || status.Mode != "apply" || status.Preflight.Mode != "query" || status.Preflight.Attempts != 1 || status.Apply == nil || status.Apply.Result.Published != 2 {
		t.Fatalf("calls=%d status=%#v", calls.Load(), status)
	}
}

func TestServiceNowPublishRetriesServerFailure(t *testing.T) {
	t.Setenv("TOPO_SERVICENOW_TOKEN", "test-token")
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"result":{"hasError":false}}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := serviceNowPublish([]string{
		"-instance", server.URL,
		"-apply",
		"-max-attempts", "2",
		"-retry-delay", "0",
	}, strings.NewReader(serviceNowObservationJSON(t)), &output, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var status serviceNowApplyStatus
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || status.Preflight.Attempts != 2 || status.Apply == nil || status.Apply.Result.Published != 2 {
		t.Fatalf("calls=%d status=%#v", calls.Load(), status)
	}
}

func TestServiceNowPublishDoesNotRetrySemanticFailureOrLeakToken(t *testing.T) {
	const token = "top-secret-token-value"
	t.Setenv("TOPO_SERVICENOW_TOKEN", token)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"result":{"items":[{"hasError":true,"error":"invalid input"}]}}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := serviceNowPublish([]string{
		"-instance", server.URL,
		"-apply",
		"-max-attempts", "5",
		"-retry-delay", "0",
	}, strings.NewReader(serviceNowObservationJSON(t)), &output, server.Client())
	if err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	combined := output.String() + err.Error()
	if strings.Contains(combined, token) {
		t.Fatal("ServiceNow token leaked through status or error")
	}
	var status serviceNowApplyStatus
	if decodeErr := json.Unmarshal(output.Bytes(), &status); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if status.Preflight.Failure == nil || status.Preflight.Failure.Retryable || status.Preflight.Exhausted || status.Apply != nil {
		t.Fatalf("status = %#v", status)
	}
}

func TestServiceNowPublishRequiresExplicitApply(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	var output bytes.Buffer
	if err := serviceNowPublish([]string{"-instance", server.URL}, strings.NewReader(serviceNowObservationJSON(t)), &output, server.Client()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("preview made %d network requests", calls.Load())
	}
}

func serviceNowObservationJSON(t *testing.T) string {
	t.Helper()
	envelope := model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: "observation-1",
		SiteID:        "site-1",
		CollectorID:   "collector-1",
		Plugin:        "local-host",
		ObservedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Assets: []model.Asset{
			{Type: model.AssetHost, NativeID: "host-1", Name: "host"},
			{Type: model.AssetNetworkInterface, NativeID: "nic-1", Name: "eth0", Attributes: map[string]any{"mac_address": "00:11:22:33:44:55"}},
		},
		Relationships: []model.Relationship{{Type: "host_has_interface", FromNativeID: "host-1", ToNativeID: "nic-1"}},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded) + "\n"
}
