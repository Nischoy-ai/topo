package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestWebhookPublish(t *testing.T) {
	var gotAuth string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusAccepted, Status: "202 Accepted", Body: io.NopCloser(strings.NewReader("accepted")), Header: make(http.Header)}, nil
	})}
	p := Publisher{Config: Config{URL: "https://cmdb.example.test/ingest", BearerToken: "secret", HTTPClient: client}}
	r, err := p.PublishBatch(context.Background(), []model.ObservationEnvelope{{ObservationID: "o1"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Published != 1 || gotAuth != "Bearer secret" {
		t.Fatalf("result=%#v auth=%q", r, gotAuth)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestWebhookRejectsHTTP(t *testing.T) {
	p := Publisher{Config: Config{URL: "http://example.com"}}
	if err := p.Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookRejectsUserinfo(t *testing.T) {
	p := Publisher{Config: Config{URL: "https://user:pass@cmdb.example.test/ingest"}}
	if err := p.Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("got %v", err)
	}
}

// TestWebhookDoesNotFollowRedirect proves defaultHTTPClient — what
// PublishBatch actually uses whenever Config.HTTPClient is nil — never
// follows a redirect, so a bearer token is never replayed against a
// destination the operator did not configure, even one on the same server.
// A plain HTTP server (rather than PublishBatch's HTTPS-only Validate path)
// keeps this focused on the redirect policy itself. TSR-2026-004.
func TestWebhookDoesNotFollowRedirect(t *testing.T) {
	var redirectTargetHit bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirected":
			redirectTargetHit = true
			w.WriteHeader(http.StatusOK)
		default:
			if r.Header.Get("Authorization") != "Bearer secret" {
				t.Error("origin should still receive the configured bearer token")
			}
			http.Redirect(w, r, server.URL+"/redirected", http.StatusFound)
		}
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
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
