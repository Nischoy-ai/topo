package mid

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	eccSOAPPath        = "/ecc_queue.do"
	maxPollRecords     = 16
	soapRequestTimeout = 30 * time.Second
	maxUsernameBytes   = 256
	maxPasswordBytes   = 64 << 10
)

// Client uses ServiceNow's direct document/literal SOAP service for
// ecc_queue. It never falls back to the JSON Table API.
type Client struct {
	endpoint   string
	username   string
	password   string
	httpClient *http.Client
}

func NewClient(instanceURL, username, password string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(instanceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Opaque != "" {
		return nil, errors.New("ServiceNow instance URL must be an absolute HTTPS origin")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("ServiceNow instance URL must not contain credentials, a path, query parameters, or a fragment")
	}
	if username == "" || len(username) > maxUsernameBytes || strings.Contains(username, ":") || hasControl(username) {
		return nil, errors.New("ServiceNow MID username is invalid")
	}
	if password == "" || len(password) > maxPasswordBytes || hasControl(password) {
		return nil, errors.New("ServiceNow MID password is invalid")
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	} else {
		clone := *httpClient
		httpClient = &clone
	}
	if httpClient.Timeout <= 0 || httpClient.Timeout > soapRequestTimeout {
		httpClient.Timeout = soapRequestTimeout
	}
	// A Basic credential must never be replayed to a destination other than
	// the exact configured instance origin, even with a caller-supplied client.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	parsed.Path = eccSOAPPath
	parsed.RawQuery = "SOAP"
	return &Client{
		endpoint:   parsed.String(),
		username:   username,
		password:   password,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Poll(ctx context.Context, agent string, limit int) ([]Record, error) {
	if !strings.HasPrefix(agent, AgentPrefix) || len(agent) > len(AgentPrefix)+maxMIDNameBytes || hasControl(agent) {
		return nil, errors.New("MID agent identity is invalid")
	}
	if limit < 1 || limit > maxPollRecords {
		return nil, fmt.Errorf("ECC poll limit must be between 1 and %d", maxPollRecords)
	}
	query := "agent=" + agent + "^queue=" + QueueOutput + "^state=" + StateReady + "^ORDERBYsys_created_on^ORDERBYsys_id"
	records, err := c.queryRecords(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := validateOutputRecord(record, agent); err != nil {
			return nil, err
		}
		if record.State != StateReady {
			return nil, errors.New("ServiceNow returned an ECC record outside output/ready")
		}
	}
	return records, nil
}

func (c *Client) Get(ctx context.Context, sysID, agent string) (Record, error) {
	if !sysIDPattern.MatchString(sysID) {
		return Record{}, errors.New("ECC sys_id is invalid")
	}
	records, err := c.queryRecords(ctx, "sys_id="+sysID+"^agent="+agent, 2)
	if err != nil {
		return Record{}, err
	}
	if len(records) != 1 {
		return Record{}, fmt.Errorf("expected one ECC record %s, got %d", sysID, len(records))
	}
	if err := validateOutputRecord(records[0], agent); err != nil {
		return Record{}, err
	}
	return records[0], nil
}

// Claim uses the native output state transition, then re-reads and verifies
// the exact record before dispatch. ServiceNow's direct SOAP update operation
// is not compare-and-swap; Run additionally requires a local process lock and
// this first slice executes no target-bearing operation.
func (c *Client) Claim(ctx context.Context, record Record) (Record, error) {
	if record.State != StateReady {
		return Record{}, errors.New("only an output/ready ECC record can be claimed")
	}
	digest, err := recordDigest(record)
	if err != nil {
		return Record{}, err
	}
	if _, err := c.mutate(ctx, "update", []soapField{
		{Name: "sys_id", Value: record.SysID},
		{Name: "state", Value: StateProcessing},
	}); err != nil {
		return Record{}, fmt.Errorf("claim ECC record: %w", err)
	}
	claimed, err := c.Get(ctx, record.SysID, record.Agent)
	if err != nil {
		return Record{}, fmt.Errorf("verify claimed ECC record: %w", err)
	}
	if claimed.State != StateProcessing {
		return Record{}, fmt.Errorf("ECC record claim did not enter %q state", StateProcessing)
	}
	claimedDigest, err := recordDigest(claimed)
	if err != nil {
		return Record{}, err
	}
	if claimedDigest != digest {
		return Record{}, errors.New("ECC record changed while it was being claimed")
	}
	return claimed, nil
}

func (c *Client) MarkProcessed(ctx context.Context, record Record) error {
	if !sysIDPattern.MatchString(record.SysID) || record.State != StateProcessing {
		return errors.New("only a claimed ECC record can be marked processed")
	}
	if _, err := c.mutate(ctx, "update", []soapField{
		{Name: "sys_id", Value: record.SysID},
		{Name: "state", Value: StateProcessed},
	}); err != nil {
		return fmt.Errorf("mark ECC record processed: %w", err)
	}
	return nil
}

func (c *Client) FindResponses(ctx context.Context, responseTo, agent string) ([]Record, error) {
	if !sysIDPattern.MatchString(responseTo) {
		return nil, errors.New("ECC response_to is invalid")
	}
	query := "response_to=" + responseTo + "^agent=" + agent + "^queue=" + QueueInput + "^ORDERBYsys_created_on^ORDERBYsys_id"
	records, err := c.queryRecords(ctx, query, 2)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if err := validateInputRecord(record, agent); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (c *Client) InsertResult(ctx context.Context, record Record) (string, error) {
	if err := validateInputRecord(record, record.Agent); err != nil {
		return "", err
	}
	fields := []soapField{
		{Name: "agent", Value: record.Agent},
		{Name: "topic", Value: record.Topic},
		{Name: "name", Value: record.Name},
		{Name: "source", Value: record.Source},
		{Name: "queue", Value: record.Queue},
		{Name: "state", Value: record.State},
		{Name: "response_to", Value: record.ResponseTo},
		{Name: "agent_correlator", Value: record.AgentCorrelator},
		{Name: "parameters", Value: record.Parameters},
		{Name: "payload", Value: record.Payload},
		{Name: "error_string", Value: record.ErrorString},
	}
	sysID, err := c.mutate(ctx, "insert", fields)
	if err != nil {
		return "", fmt.Errorf("insert ECC result: %w", err)
	}
	return sysID, nil
}

func (c *Client) queryRecords(ctx context.Context, encodedQuery string, limit int) ([]Record, error) {
	if limit < 1 || limit > maxPollRecords || len(encodedQuery) > 2048 || hasControl(encodedQuery) {
		return nil, errors.New("ECC encoded query is invalid")
	}
	response, err := c.call(ctx, "getRecords", []soapField{
		{Name: "__encoded_query", Value: encodedQuery},
		{Name: "__first_row", Value: "0"},
		{Name: "__last_row", Value: strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	if response.Body.GetRecordsResponse == nil {
		return nil, errors.New("ServiceNow SOAP getRecords response is missing getRecordsResponse")
	}
	if len(response.Body.GetRecordsResponse.Records) > limit {
		return nil, fmt.Errorf("ServiceNow returned %d ECC records; requested at most %d", len(response.Body.GetRecordsResponse.Records), limit)
	}
	records := make([]Record, 0, len(response.Body.GetRecordsResponse.Records))
	for _, wireRecord := range response.Body.GetRecordsResponse.Records {
		record := wireRecord.model()
		if err := validateRecordFields(record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (c *Client) mutate(ctx context.Context, operation string, fields []soapField) (string, error) {
	response, err := c.call(ctx, operation, fields)
	if err != nil {
		return "", err
	}
	var mutation *mutationResponse
	switch operation {
	case "insert":
		mutation = response.Body.InsertResponse
	case "update":
		mutation = response.Body.UpdateResponse
	default:
		return "", fmt.Errorf("unsupported SOAP mutation %q", operation)
	}
	if mutation == nil || !sysIDPattern.MatchString(strings.TrimSpace(mutation.SysID)) {
		return "", fmt.Errorf("ServiceNow SOAP %s response has an invalid sys_id", operation)
	}
	return strings.ToLower(strings.TrimSpace(mutation.SysID)), nil
}

func (c *Client) call(ctx context.Context, operation string, fields []soapField) (soapEnvelope, error) {
	body, err := encodeSOAPRequest(operation, fields)
	if err != nil {
		return soapEnvelope{}, fmt.Errorf("encode ServiceNow SOAP %s request: %w", operation, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return soapEnvelope{}, fmt.Errorf("build ServiceNow SOAP %s request: %w", operation, err)
	}
	request.SetBasicAuth(c.username, c.password)
	request.Header.Set("Content-Type", "text/xml; charset=utf-8")
	request.Header.Set("Accept", "text/xml")
	request.Header.Set("SOAPAction", eccQueueNamespace+"/"+operation)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return soapEnvelope{}, fmt.Errorf("send ServiceNow SOAP %s request: %w", operation, err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxSOAPResponseBytes+1)
	responseBody, readErr := io.ReadAll(limited)
	if readErr != nil {
		return soapEnvelope{}, fmt.Errorf("read ServiceNow SOAP %s response: %w", operation, readErr)
	}
	if len(responseBody) > maxSOAPResponseBytes {
		return soapEnvelope{}, fmt.Errorf("ServiceNow SOAP response exceeds %d bytes", maxSOAPResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if _, decodeErr := decodeSOAPEnvelope(responseBody); decodeErr != nil && strings.Contains(decodeErr.Error(), "SOAP fault") {
			return soapEnvelope{}, fmt.Errorf("ServiceNow SOAP %s returned HTTP %d: %w", operation, response.StatusCode, decodeErr)
		}
		return soapEnvelope{}, fmt.Errorf("ServiceNow SOAP %s returned HTTP %d", operation, response.StatusCode)
	}
	envelope, err := decodeSOAPEnvelope(responseBody)
	if err != nil {
		return soapEnvelope{}, err
	}
	return envelope, nil
}
