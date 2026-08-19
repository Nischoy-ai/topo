package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

// maxSenderResponseBytes bounds how much of a controller error response the
// sender will read.
const maxSenderResponseBytes = 64 << 10

// requestTimeout bounds a single delivery attempt, independent of whatever
// deadline the caller's context already carries.
const requestTimeout = 30 * time.Second

// DeliveryError reports whether a failed delivery is worth retrying.
// Non-retryable failures (the controller rejected the payload itself) are
// dropped by the run loop rather than retried forever.
type DeliveryError struct {
	Retryable bool
	err       error
}

func (e *DeliveryError) Error() string { return e.err.Error() }
func (e *DeliveryError) Unwrap() error { return e.err }

// Sender delivers observation envelopes to a Topo Hub controller's ingest
// endpoint one at a time, matching the controller's single-object wire
// format.
type Sender struct {
	ingestURL  string
	apiKey     string
	httpClient *http.Client
}

// NewSender builds a Sender targeting controllerURL, the controller's base
// address (for example the value passed to `topo serve -addr`). tlsConfig
// is optional; pass one built with enrollment.LoadClientTLSConfig to
// authenticate with an enrolled certificate over outbound mTLS instead of,
// or alongside, the bearer API key. When tlsConfig is nil and controllerURL
// is plain HTTP, place a TLS-terminating reverse proxy in front of the
// controller for production deployments, matching how `topo serve` is
// documented today.
func NewSender(controllerURL, apiKey string, tlsConfig *tls.Config) (*Sender, error) {
	parsed, err := url.Parse(controllerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("controller URL must be an absolute http:// or https:// URL")
	}
	ingest := strings.TrimRight(controllerURL, "/") + "/v1/observations"
	httpClient := &http.Client{Timeout: requestTimeout}
	if tlsConfig != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		httpClient.Transport = transport
	}
	return &Sender{
		ingestURL:  ingest,
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

// Send delivers one observation envelope. The returned error is a
// *DeliveryError when the failure's retryability is known.
func (s *Sender) Send(ctx context.Context, envelope model.ObservationEnvelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ingestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build delivery request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	response, err := s.httpClient.Do(request)
	if err != nil {
		return &DeliveryError{Retryable: true, err: fmt.Errorf("deliver observation: %w", err)}
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxSenderResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode >= 500
		return &DeliveryError{Retryable: retryable, err: fmt.Errorf("controller returned %s: %s", response.Status, responseBody)}
	}
	return nil
}
