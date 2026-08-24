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

// Sender delivers observation envelopes, heartbeats, and job polls/results
// to a Topo Hub controller one at a time, matching the controller's
// single-object wire format.
type Sender struct {
	ingestURL    string
	heartbeatURL string
	jobsURL      string
	apiKey       string
	httpClient   *http.Client
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
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("controller URL must not contain credentials, a path, query parameters, or a fragment")
	}
	base := strings.TrimRight(controllerURL, "/")
	httpClient := &http.Client{
		Timeout: requestTimeout,
		// Never follow a redirect: the bearer key/mTLS identity below must
		// not be replayed against a host the operator did not configure.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if tlsConfig != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = tlsConfig
		httpClient.Transport = transport
	}
	return &Sender{
		ingestURL:    base + "/v1/observations",
		heartbeatURL: base + "/v1/heartbeats",
		jobsURL:      base + "/v1/jobs",
		apiKey:       apiKey,
		httpClient:   httpClient,
	}, nil
}

// Send delivers one observation envelope. The returned error is a
// *DeliveryError when the failure's retryability is known.
func (s *Sender) Send(ctx context.Context, envelope model.ObservationEnvelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	return s.post(ctx, s.ingestURL, body)
}

// SendHeartbeat posts a lightweight liveness signal, distinct from
// observation delivery, so the controller can tell the collector is alive
// between discovery scans. The returned error is a *DeliveryError when the
// failure's retryability is known; callers are not expected to buffer a
// failed heartbeat the way Send's caller buffers a failed observation —
// a stale heartbeat has no lasting value once superseded by the next one.
func (s *Sender) SendHeartbeat(ctx context.Context, collectorID, siteID string) error {
	body, err := json.Marshal(model.HeartbeatRequest{SchemaVersion: model.SchemaVersion, CollectorID: collectorID, SiteID: siteID})
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}
	return s.post(ctx, s.heartbeatURL, body)
}

// PollJobs fetches and marks-dispatched every job the controller has
// queued for collectorID, so this Sender must not be shared between
// collectors. There is no analog to a heartbeat's silent best-effort
// failure here: a poll error is returned to the caller, since a missed
// poll means any queued job simply waits for the next one.
func (s *Sender) PollJobs(ctx context.Context, collectorID string) ([]model.Job, error) {
	requestURL := s.jobsURL + "?collector_id=" + url.QueryEscape(collectorID)
	var decoded struct {
		Items []model.Job `json:"items"`
	}
	if err := s.get(ctx, requestURL, &decoded); err != nil {
		return nil, err
	}
	return decoded.Items, nil
}

// ReportJobResult tells the controller how a dispatched job went.
func (s *Sender) ReportJobResult(ctx context.Context, collectorID, jobID string, result model.JobResult) error {
	result.CollectorID = collectorID
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal job result: %w", err)
	}
	return s.post(ctx, s.jobsURL+"/"+url.PathEscape(jobID)+"/result", body)
}

// post issues one authenticated POST and classifies the result the same
// way for every Sender method: a transport-level failure or 5xx is
// retryable, a 4xx is not.
func (s *Sender) post(ctx context.Context, requestURL string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return s.do(request)
}

// get issues one authenticated GET and decodes a successful JSON response
// into out.
func (s *Sender) get(ctx context.Context, requestURL string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	response, err := s.doRaw(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, maxSenderResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// do runs request via doRaw and discards a successful response body,
// for callers that only care whether the request succeeded.
func (s *Sender) do(request *http.Request) error {
	response, err := s.doRaw(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxSenderResponseBytes))
	return nil
}

// doRaw authenticates and sends request, returning a *DeliveryError for a
// transport failure or a non-2xx status (5xx and transport failures are
// retryable, 4xx is not). The caller must close the response body, and
// consume at most maxSenderResponseBytes of it, on a nil error.
func (s *Sender) doRaw(request *http.Request) (*http.Response, error) {
	request.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, &DeliveryError{Retryable: true, err: fmt.Errorf("deliver request: %w", err)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxSenderResponseBytes))
		retryable := response.StatusCode >= 500
		return nil, &DeliveryError{Retryable: retryable, err: fmt.Errorf("controller returned %s: %s", response.Status, responseBody)}
	}
	return response, nil
}
