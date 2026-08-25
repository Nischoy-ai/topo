package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	pollPath               = "/api/x_nischoy_topo/v1/relay/poll"
	resultPath             = "/api/x_nischoy_topo/v1/relay/result"
	maxControlResponseSize = 1 << 20
	maxJobsPerPoll         = 1
	controlRequestTimeout  = 30 * time.Second
)

// DeliveryError classifies control-plane failures so the run loop can keep
// retrying transient transport/5xx failures without treating a rejected job
// result as successfully recorded.
type DeliveryError struct {
	Retryable bool
	err       error
}

func (e *DeliveryError) Error() string { return e.err.Error() }
func (e *DeliveryError) Unwrap() error { return e.err }

// Client talks only to the two fixed endpoints installed by the Topo
// ServiceNow application.
type Client struct {
	pollURL    string
	resultURL  string
	token      string
	httpClient *http.Client
}

func NewClient(instanceURL, token string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(instanceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("ServiceNow instance URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("ServiceNow instance URL must not contain credentials, a path, query parameters, or a fragment")
	}
	if token == "" {
		return nil, errors.New("ServiceNow bearer token is required")
	}
	if len(token) > 64<<10 || strings.IndexFunc(token, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return nil, errors.New("ServiceNow bearer token is invalid")
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: controlRequestTimeout,
		}
	} else {
		clone := *httpClient
		httpClient = &clone
	}
	// Never follow a redirect, even when a caller supplies a custom transport:
	// the Relay bearer token must not be replayed to an unconfigured host.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	base := strings.TrimRight(instanceURL, "/")
	return &Client{pollURL: base + pollPath, resultURL: base + resultPath, token: token, httpClient: httpClient}, nil
}

func (c *Client) Poll(ctx context.Context, request PollRequest) ([]Job, error) {
	var response pollResponse
	if err := c.postJSON(ctx, c.pollURL, request, &response); err != nil {
		return nil, err
	}
	if len(response.Jobs) > maxJobsPerPoll {
		return nil, fmt.Errorf("ServiceNow returned %d jobs; maximum is %d", len(response.Jobs), maxJobsPerPoll)
	}
	for _, job := range response.Jobs {
		if err := validateJob(job); err != nil {
			return nil, err
		}
	}
	return response.Jobs, nil
}

func (c *Client) Report(ctx context.Context, result JobResult) error {
	return c.postJSON(ctx, c.resultURL, result, nil)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode ServiceNow request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build ServiceNow request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return &DeliveryError{Retryable: true, err: fmt.Errorf("send ServiceNow request: %w", err)}
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxControlResponseSize+1)
	responseBody, readErr := io.ReadAll(limited)
	if readErr != nil {
		return &DeliveryError{Retryable: true, err: fmt.Errorf("read ServiceNow response: %w", readErr)}
	}
	if len(responseBody) > maxControlResponseSize {
		return &DeliveryError{Retryable: false, err: fmt.Errorf("ServiceNow response exceeds %d bytes", maxControlResponseSize)}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &DeliveryError{
			Retryable: response.StatusCode >= 500,
			err:       fmt.Errorf("ServiceNow returned %s: %s", response.Status, truncate(string(responseBody), 4096)),
		}
	}
	if output == nil || len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode ServiceNow response: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("decode ServiceNow response: multiple JSON values")
	}
	return nil
}

func validateJob(job Job) error {
	if job.JobID == "" || len(job.JobID) > 128 {
		return errors.New("ServiceNow job_id must be between 1 and 128 characters")
	}
	if job.Type != JobTypeDiscover {
		return fmt.Errorf("ServiceNow job %q has unsupported type %q", job.JobID, job.Type)
	}
	if job.ProfileID == "" || len(job.ProfileID) > 128 {
		return fmt.Errorf("ServiceNow job %q profile_id must be between 1 and 128 characters", job.JobID)
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
