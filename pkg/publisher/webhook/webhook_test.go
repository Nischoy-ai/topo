package webhook

import (
	"context"
	"io"
	"net/http"
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
