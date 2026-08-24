package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

type Config struct {
	URL         string
	BearerToken string
	HTTPClient  *http.Client
}

type Publisher struct{ Config Config }

func (p Publisher) Validate(context.Context) error {
	u, err := url.Parse(p.Config.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("webhook URL must be an absolute HTTPS URL")
	}
	if u.User != nil {
		return errors.New("webhook URL must not embed credentials")
	}
	return nil
}

func (p Publisher) Preview(_ context.Context, envelopes []model.ObservationEnvelope) (any, error) {
	return envelopes, nil
}

func (p Publisher) PublishBatch(ctx context.Context, envelopes []model.ObservationEnvelope) (publisher.Result, error) {
	if err := p.Validate(ctx); err != nil {
		return publisher.Result{}, err
	}
	b, err := json.Marshal(envelopes)
	if err != nil {
		return publisher.Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Config.URL, bytes.NewReader(b))
	if err != nil {
		return publisher.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Config.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.Config.BearerToken)
	}
	client := p.Config.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return publisher.Result{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return publisher.Result{Destination: "webhook", Rejected: len(envelopes)}, fmt.Errorf("webhook returned %s: %s", resp.Status, body)
	}
	return publisher.Result{Destination: "webhook", Published: len(envelopes)}, nil
}

// defaultHTTPClient is used whenever Config.HTTPClient is nil. It never
// follows a redirect: the Authorization header set above must not be
// replayed against a destination the operator did not configure.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
