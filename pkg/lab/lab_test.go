package lab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
)

func TestGenerateIsDeterministic(t *testing.T) {
	s := DefaultScenario(500, 30, 42)
	a, err := Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Hosts) != 500 || len(a.Expected.Assets) != 1000 {
		t.Fatalf("hosts=%d assets=%d", len(a.Hosts), len(a.Expected.Assets))
	}
	if !reflect.DeepEqual(a.Hosts[123], b.Hosts[123]) {
		t.Fatal("same seed generated different host")
	}
}

func TestScenarioJSONRoundTripAndStrictFields(t *testing.T) {
	s := DefaultScenario(10, 20, 7)
	var b strings.Builder
	if err := EncodeScenario(&b, s); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeScenario(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Seed != 7 || len(decoded.Hosts) != 2 {
		t.Fatalf("unexpected scenario: %#v", decoded)
	}
	if _, err := DecodeScenario(strings.NewReader(`{"version":"v1alpha1","site_id":"lab","seed":1,"unknown":true,"hosts":[{"persona":"ubuntu-24.04","count":1}]}`)); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestScenarioRejectsFaultTotalsOverOneHundred(t *testing.T) {
	s := DefaultScenario(1, 0, 1)
	s.Faults = Faults{AuthFailurePercent: 60, TimeoutPercent: 50}
	if err := s.Validate(); err == nil {
		t.Fatal("expected invalid total fault percentage")
	}
}

func TestDiscoverFiveHundredHostsAndIdempotentResolution(t *testing.T) {
	s := DefaultScenario(500, 30, 42)
	estate, err := Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: handlerTransport{handler: NewServer(estate).Handler()}}
	first, err := Discover(context.Background(), RunOptions{BaseURL: "http://lab.test", Concurrency: 64, HTTPClient: client, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if first.Discovered != 500 || first.Failed != 0 {
		t.Fatalf("unexpected result: %#v", first)
	}
	evaluation := Evaluate(estate.Expected, first.Observation)
	if evaluation.CoveragePercent != 100 || evaluation.MissingAssets != 0 || evaluation.UnexpectedAssets != 0 {
		t.Fatalf("unexpected evaluation: %#v", evaluation)
	}
	repo := store.NewMemory()
	if err := repo.SaveObservation(context.Background(), first.Observation); err != nil {
		t.Fatal(err)
	}
	second, err := Discover(context.Background(), RunOptions{BaseURL: "http://lab.test", Concurrency: 64, HTTPClient: client, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveObservation(context.Background(), second.Observation); err != nil {
		t.Fatal(err)
	}
	assets, err := repo.ListAssets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1000 {
		t.Fatalf("repeated scan created duplicates: got %d assets", len(assets))
	}
}

func TestFaultInjectionProducesPartialResults(t *testing.T) {
	s := DefaultScenario(100, 50, 99)
	s.Faults = Faults{AuthFailurePercent: 20, PermissionDeniedPercent: 20, MalformedPercent: 20, DisappearPercent: 20}
	estate, err := Generate(s)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: handlerTransport{handler: NewServer(estate).Handler()}}
	result, err := Discover(context.Background(), RunOptions{BaseURL: "http://lab.test", Concurrency: 20, HTTPClient: client, RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed == 0 || result.Partial == 0 || result.Discovered == 100 {
		t.Fatalf("faults not exercised: %#v", result)
	}
}

type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if resp.Body == nil {
		resp.Body = io.NopCloser(strings.NewReader(""))
	}
	resp.Request = req
	return resp, nil
}
