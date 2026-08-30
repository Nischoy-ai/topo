package worker

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	registerPath           = "/api/x_664635_topo/v1/tasks/workers/register"
	heartbeatPath          = "/api/x_664635_topo/v1/tasks/workers/heartbeat"
	claimPath              = "/api/x_664635_topo/v1/tasks/claim"
	taskPathPrefix         = "/api/x_664635_topo/v1/tasks/"
	maxControlRequestBytes = 2 << 20
	maxControlResponseSize = 1 << 20
	controlRequestTimeout  = 30 * time.Second
)

type HTTPError struct {
	StatusCode int
	Retryable  bool
	err        error
}

type serviceNowResultEnvelope struct {
	Result json.RawMessage `json:"result"`
}

func (e *HTTPError) Error() string { return e.err.Error() }
func (e *HTTPError) Unwrap() error { return e.err }

// Client calls only the fixed worker resources exposed by the Nischoy Topo
// scoped application. It has no generic Table API or IRE methods.
type Client struct {
	baseURL    string
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
	if !validToken(token) {
		return nil, errors.New("ServiceNow worker bearer token is invalid")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: controlRequestTimeout}
	} else {
		clone := *httpClient
		httpClient = &clone
		if httpClient.Timeout <= 0 || httpClient.Timeout > controlRequestTimeout {
			httpClient.Timeout = controlRequestTimeout
		}
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{baseURL: strings.TrimRight(instanceURL, "/"), token: token, httpClient: httpClient}, nil
}

func (c *Client) Register(ctx context.Context, request RegisterRequest) (RegisterResponse, error) {
	var response RegisterResponse
	err := c.post(ctx, registerPath, request, &response)
	return response, err
}

func (c *Client) Heartbeat(ctx context.Context, request HeartbeatRequest) (HeartbeatResponse, error) {
	var response HeartbeatResponse
	err := c.post(ctx, heartbeatPath, request, &response)
	return response, err
}

func (c *Client) Claim(ctx context.Context, request ClaimRequest) (ClaimResponse, error) {
	var response ClaimResponse
	err := c.post(ctx, claimPath, request, &response)
	if err != nil {
		return ClaimResponse{}, err
	}
	if response.Task != nil {
		if err := validateTask(*response.Task); err != nil {
			return ClaimResponse{}, err
		}
	}
	return response, nil
}

func (c *Client) Renew(ctx context.Context, taskID string, request RenewRequest) (RenewResponse, error) {
	if !safeID.MatchString(taskID) {
		return RenewResponse{}, errors.New("task ID is invalid")
	}
	var response RenewResponse
	err := c.post(ctx, taskPathPrefix+taskID+"/renew", request, &response)
	return response, err
}

func (c *Client) Credential(ctx context.Context, taskID string, request CredentialRequest) (SSHCredential, error) {
	if !safeID.MatchString(taskID) {
		return SSHCredential{}, errors.New("task ID is invalid")
	}
	var response SSHCredential
	headers, err := c.postResponse(ctx, taskPathPrefix+taskID+"/credential", request, &response)
	if err != nil {
		return SSHCredential{}, err
	}
	if !headerHasNoStore(headers) {
		return SSHCredential{}, errors.New("ServiceNow credential response is missing Cache-Control: no-store")
	}
	if !safeSSHUsername(response.Username) || response.Password == "" || len(response.Password) > 4096 {
		return SSHCredential{}, errors.New("ServiceNow returned an invalid SSH credential")
	}
	return response, nil
}

func (c *Client) SubmitResult(ctx context.Context, taskID string, request ResultChunkRequest) (ResultChunkResponse, error) {
	if !safeID.MatchString(taskID) {
		return ResultChunkResponse{}, errors.New("task ID is invalid")
	}
	var response ResultChunkResponse
	err := c.post(ctx, taskPathPrefix+taskID+"/results", request, &response)
	return response, err
}

func (c *Client) Complete(ctx context.Context, taskID string, request CompleteRequest) (CompleteResponse, error) {
	if !safeID.MatchString(taskID) {
		return CompleteResponse{}, errors.New("task ID is invalid")
	}
	var response CompleteResponse
	err := c.post(ctx, taskPathPrefix+taskID+"/complete", request, &response)
	return response, err
}

func (c *Client) post(ctx context.Context, path string, input, output any) error {
	_, err := c.postResponse(ctx, path, input, output)
	return err
}

func (c *Client) postResponse(ctx context.Context, path string, input, output any) (http.Header, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode ServiceNow worker request: %w", err)
	}
	if len(body) > maxControlRequestBytes {
		return nil, fmt.Errorf("ServiceNow worker request exceeds %d bytes", maxControlRequestBytes)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build ServiceNow worker request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, &HTTPError{Retryable: true, err: fmt.Errorf("send ServiceNow worker request: %w", err)}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxControlResponseSize+1))
	if err != nil {
		return nil, &HTTPError{Retryable: true, err: fmt.Errorf("read ServiceNow worker response: %w", err)}
	}
	if len(responseBody) > maxControlResponseSize {
		return nil, &HTTPError{StatusCode: response.StatusCode, err: fmt.Errorf("ServiceNow worker response exceeds %d bytes", maxControlResponseSize)}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &HTTPError{
			StatusCode: response.StatusCode,
			Retryable:  response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError,
			// The remote response is untrusted and may reflect a request field.
			// Do not put it in an error that the worker will log: task attempts
			// carry a raw lease token which must exist only in bounded memory.
			err: fmt.Errorf("ServiceNow worker API returned %s", response.Status),
		}
	}
	if output == nil {
		return response.Header.Clone(), nil
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil, errors.New("ServiceNow worker API returned an empty response")
	}
	var envelope serviceNowResultEnvelope
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode ServiceNow worker response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("decode ServiceNow worker response: %w", err)
	}
	if len(bytes.TrimSpace(envelope.Result)) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
		return nil, errors.New("ServiceNow worker API returned an empty result")
	}
	decoder = json.NewDecoder(bytes.NewReader(envelope.Result))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return nil, fmt.Errorf("decode ServiceNow worker result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("decode ServiceNow worker result: %w", err)
	}
	return response.Header.Clone(), nil
}

func validateTask(task Task) error {
	for label, value := range map[string]string{
		"task_id":     task.TaskID,
		"run_id":      task.RunID,
		"attempt_id":  task.AttemptID,
		"profile_id":  task.ProfileID,
		"lease_token": task.LeaseToken,
	} {
		if !safeID.MatchString(value) {
			return fmt.Errorf("ServiceNow task %s is invalid", label)
		}
	}
	if task.Operation != OperationLocalV1 && task.Operation != OperationSSHLinuxV1 {
		return fmt.Errorf("ServiceNow task %q has unsupported operation %q", task.TaskID, task.Operation)
	}
	if task.ProfileRevision < 1 {
		return fmt.Errorf("ServiceNow task %q profile_revision must be positive", task.TaskID)
	}
	if err := validateTargetPartition(task.TargetPartition); err != nil {
		return fmt.Errorf("ServiceNow task %q target partition is invalid: %w", task.TaskID, err)
	}
	if task.LeaseExpiresAt.IsZero() || task.Deadline.IsZero() {
		return fmt.Errorf("ServiceNow task %q is missing its lease or deadline", task.TaskID)
	}
	if !task.LeaseExpiresAt.After(time.Now().UTC()) || task.LeaseExpiresAt.After(task.Deadline) {
		return fmt.Errorf("ServiceNow task %q has an expired or out-of-bounds lease", task.TaskID)
	}
	switch task.Operation {
	case OperationLocalV1:
		if task.CredentialBindingID != "" {
			return fmt.Errorf("ServiceNow local task %q contains credential authority", task.TaskID)
		}
	case OperationSSHLinuxV1:
		if !safeID.MatchString(task.CredentialBindingID) {
			return fmt.Errorf("ServiceNow SSH task %q credential binding is invalid", task.TaskID)
		}
		if task.TargetPartition == nil || task.TargetPartition.Count > maxSSHTargets || len(task.TargetPartition.CIDRs) != 1 {
			return fmt.Errorf("ServiceNow SSH task %q must contain one bounded target", task.TaskID)
		}
		prefix, err := netip.ParsePrefix(task.TargetPartition.CIDRs[0])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return fmt.Errorf("ServiceNow SSH task %q target must be an IPv4 /32", task.TaskID)
		}
	}
	return nil
}

func headerHasNoStore(headers http.Header) bool {
	for _, directive := range strings.Split(headers.Get("Cache-Control"), ",") {
		if strings.EqualFold(strings.TrimSpace(directive), "no-store") {
			return true
		}
	}
	return false
}

func validateTargetPartition(partition *TargetPartition) error {
	if partition == nil {
		return nil
	}
	if len(partition.Key) != sha256HexLength {
		return errors.New("partition key is invalid")
	}
	if _, err := hex.DecodeString(partition.Key); err != nil {
		return errors.New("partition key is invalid")
	}
	if partition.Count < 1 || partition.Count > DefaultMaxPartitions || partition.Ordinal < 0 || partition.Ordinal >= partition.Count {
		return errors.New("partition ordinal or count is invalid")
	}
	if len(partition.CIDRs) < 1 || len(partition.CIDRs) > maxPartitionCIDRs {
		return fmt.Errorf("partition must contain 1-%d CIDRs", maxPartitionCIDRs)
	}
	for _, value := range partition.CIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Zone() != "" || prefix != prefix.Masked() || prefix.String() != value {
			return fmt.Errorf("partition CIDR %q is not canonical", value)
		}
	}
	return nil
}

func validToken(token string) bool {
	if token == "" || len(token) > 64<<10 {
		return false
	}
	return strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
