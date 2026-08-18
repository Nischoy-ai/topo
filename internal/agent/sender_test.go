package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestSenderSendPostsSingleEnvelope(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody model.ObservationEnvelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "test-api-key")
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope("obs-1")
	if err := sender.Send(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/observations" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotBody.ObservationID != envelope.ObservationID {
		t.Fatalf("observation id = %q", gotBody.ObservationID)
	}
}

func TestSenderServerErrorIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testEnvelope("obs-1"))
	var delivery *DeliveryError
	if !errors.As(err, &delivery) {
		t.Fatalf("error = %v, want *DeliveryError", err)
	}
	if !delivery.Retryable {
		t.Fatal("expected a 503 to be retryable")
	}
}

func TestSenderClientErrorIsNotRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("invalid observation"))
	}))
	defer server.Close()

	sender, err := NewSender(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), testEnvelope("obs-1"))
	var delivery *DeliveryError
	if !errors.As(err, &delivery) {
		t.Fatalf("error = %v, want *DeliveryError", err)
	}
	if delivery.Retryable {
		t.Fatal("expected a 422 to be non-retryable")
	}
	if !strings.Contains(err.Error(), "invalid observation") {
		t.Fatalf("error = %v", err)
	}
}

func TestSenderHonorsContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer func() {
		close(blocked)
		server.Close()
	}()

	sender, err := NewSender(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sender.Send(ctx, testEnvelope("obs-1")); err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestNewSenderRejectsInvalidURL(t *testing.T) {
	if _, err := NewSender("not-a-url", ""); err == nil {
		t.Fatal("expected an invalid controller URL to be rejected")
	}
	if _, err := NewSender("ftp://example.test", ""); err == nil {
		t.Fatal("expected a non-http(s) scheme to be rejected")
	}
}
