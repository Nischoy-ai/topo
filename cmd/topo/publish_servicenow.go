package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Nischoy-ai/topo/internal/irepublish"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
)

const maxServiceNowPublishTimeout = 10 * time.Minute

type serviceNowPreviewStatus struct {
	Mode      string                `json:"mode"`
	Envelopes int                   `json:"envelopes"`
	Items     int                   `json:"items"`
	Relations int                   `json:"relations"`
	Payload   servicenow.IREPayload `json:"payload"`
}

type serviceNowApplyStatus struct {
	Mode      string             `json:"mode"`
	Preflight irepublish.Status  `json:"preflight"`
	Apply     *irepublish.Status `json:"apply,omitempty"`
}

func runPublish(args []string) error {
	if len(args) == 0 || args[0] != "servicenow" {
		return errors.New("usage: topo publish servicenow")
	}
	return serviceNowPublish(args[1:], os.Stdin, os.Stdout, nil)
}

func serviceNowPublish(args []string, stdin io.Reader, stdout io.Writer, httpClient *http.Client) error {
	fs := flag.NewFlagSet("publish servicenow", flag.ContinueOnError)
	instanceURL := env("SERVICENOW_INSTANCE_URL", "")
	fs.StringVar(&instanceURL, "instance", instanceURL, "ServiceNow instance origin (absolute HTTPS URL)")
	fs.StringVar(&instanceURL, "servicenow-instance", instanceURL, "deprecated alias for -instance")
	inputPath := fs.String("input", "-", "Topo observation JSON Lines file, or - for stdin")
	tokenRef := fs.String("token-ref", "", "credential reference for the ServiceNow bearer token (env:, file:, vault:, or k8s:); apply mode only")
	discoverySource := fs.String("discovery-source", "Nischoy Topo", "registered ServiceNow discovery_source choice value")
	apply := fs.Bool("apply", false, "publish to IRE; without this flag only a local preview is produced")
	timeout := fs.Duration("timeout", 2*time.Minute, "overall bounded publication deadline")
	maxAttempts := fs.Int("max-attempts", 3, "maximum apply attempts for explicitly retryable failures (1-5)")
	retryDelay := fs.Duration("retry-delay", time.Second, "initial retry delay with bounded exponential backoff")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("topo publish servicenow does not accept positional arguments")
	}
	if instanceURL == "" {
		return errors.New("-instance is required")
	}
	if *timeout <= 0 || *timeout > maxServiceNowPublishTimeout {
		return fmt.Errorf("-timeout must be greater than zero and at most %s", maxServiceNowPublishTimeout)
	}
	if *maxAttempts < 1 || *maxAttempts > irepublish.MaxAttempts {
		return fmt.Errorf("-max-attempts must be between 1 and %d", irepublish.MaxAttempts)
	}
	if *retryDelay < 0 || *retryDelay > irepublish.MaxRetryDelay {
		return fmt.Errorf("-retry-delay must be between 0 and %s", irepublish.MaxRetryDelay)
	}

	input, closeInput, err := openObservationInput(*inputPath, stdin)
	if err != nil {
		return err
	}
	if closeInput != nil {
		defer closeInput()
	}
	envelopes, err := servicenow.DecodeJSONLines(input)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	previewPublisher := servicenow.Publisher{Config: servicenow.Config{
		InstanceURL:     instanceURL,
		DiscoverySource: *discoverySource,
		DryRun:          true,
		HTTPClient:      httpClient,
	}}
	previewValue, err := previewPublisher.Preview(ctx, envelopes)
	if err != nil {
		return err
	}
	payload, ok := previewValue.(servicenow.IREPayload)
	if !ok {
		return errors.New("ServiceNow preview returned an unexpected payload type")
	}
	if !*apply {
		return encodeServiceNowStatus(stdout, serviceNowPreviewStatus{
			Mode:      "preview",
			Envelopes: len(envelopes),
			Items:     len(payload.Items),
			Relations: len(payload.Relations),
			Payload:   payload,
		})
	}

	token, err := resolveCredential(*tokenRef, "", "TOPO_SERVICENOW_TOKEN", false)
	if err != nil {
		return fmt.Errorf("resolve ServiceNow token: %w", err)
	}
	defer clear(token)
	publish := servicenow.Publisher{Config: servicenow.Config{
		InstanceURL:     instanceURL,
		Token:           string(token),
		DiscoverySource: *discoverySource,
		HTTPClient:      httpClient,
	}}
	preflight, preflightErr := irepublish.Publish(ctx, irepublish.BatchPublisherFunc(publish.QueryBatch), envelopes, *maxAttempts, *retryDelay)
	preflight.Mode = "query"
	status := serviceNowApplyStatus{Mode: "apply", Preflight: preflight}
	if preflightErr != nil {
		if err := encodeServiceNowStatus(stdout, status); err != nil {
			return err
		}
		return fmt.Errorf("ServiceNow IRE preflight failed: %w", preflightErr)
	}
	applyStatus, publishErr := irepublish.Publish(ctx, publish, envelopes, *maxAttempts, *retryDelay)
	status.Apply = &applyStatus
	if err := encodeServiceNowStatus(stdout, status); err != nil {
		return err
	}
	return publishErr
}

func openObservationInput(path string, stdin io.Reader) (io.Reader, func(), error) {
	if path == "-" {
		if stdin == nil {
			return nil, nil, errors.New("stdin is unavailable")
		}
		return stdin, nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open observation input: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("inspect observation input: %w", err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("observation input must be a regular file")
	}
	return file, func() { _ = file.Close() }, nil
}

func encodeServiceNowStatus(output io.Writer, status any) error {
	if output == nil {
		return errors.New("ServiceNow status output is unavailable")
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(status); err != nil {
		return fmt.Errorf("write ServiceNow publication status: %w", err)
	}
	return nil
}
