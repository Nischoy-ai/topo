// Package irepublish implements the bounded retry policy for the supported
// ServiceNow IRE operator workflow.
package irepublish

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

const (
	MaxAttempts   = 5
	MaxRetryDelay = 30 * time.Second
)

type BatchPublisher interface {
	PublishBatch(context.Context, []model.ObservationEnvelope) (publisher.Result, error)
}

type BatchPublisherFunc func(context.Context, []model.ObservationEnvelope) (publisher.Result, error)

func (f BatchPublisherFunc) PublishBatch(ctx context.Context, envelopes []model.ObservationEnvelope) (publisher.Result, error) {
	return f(ctx, envelopes)
}

type Failure struct {
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type Status struct {
	Mode      string           `json:"mode"`
	Attempts  int              `json:"attempts"`
	Result    publisher.Result `json:"result"`
	Failure   *Failure         `json:"failure,omitempty"`
	Exhausted bool             `json:"exhausted,omitempty"`
}

// Publish retries only failures explicitly classified as retryable by the IRE
// publisher. IRE semantic errors and ambiguous successful responses are never
// replayed automatically because the first request may have left state behind.
func Publish(ctx context.Context, batchPublisher BatchPublisher, envelopes []model.ObservationEnvelope, maxAttempts int, initialDelay time.Duration) (Status, error) {
	if batchPublisher == nil {
		return Status{}, errors.New("ServiceNow IRE publisher is required")
	}
	if maxAttempts < 1 || maxAttempts > MaxAttempts {
		return Status{}, fmt.Errorf("ServiceNow max attempts must be between 1 and %d", MaxAttempts)
	}
	if initialDelay < 0 || initialDelay > MaxRetryDelay {
		return Status{}, fmt.Errorf("ServiceNow retry delay must be between 0 and %s", MaxRetryDelay)
	}

	status := Status{Mode: "apply"}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status.Attempts = attempt
		result, err := batchPublisher.PublishBatch(ctx, envelopes)
		status.Result = result
		if err == nil {
			status.Failure = nil
			return status, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			status.Failure = &Failure{Message: ctxErr.Error(), Retryable: false}
			return status, ctxErr
		}

		retryable := isRetryable(err)
		status.Failure = &Failure{Message: err.Error(), Retryable: retryable}
		if !retryable {
			return status, err
		}
		if attempt == maxAttempts {
			status.Exhausted = true
			return status, err
		}

		delay := retryDelay(initialDelay, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			status.Failure = &Failure{Message: ctx.Err().Error(), Retryable: false}
			return status, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func isRetryable(err error) bool {
	var classified interface{ Retryable() bool }
	return errors.As(err, &classified) && classified.Retryable()
}

func retryDelay(initial time.Duration, attempt int) time.Duration {
	delay := initial
	for i := 1; i < attempt && delay < MaxRetryDelay; i++ {
		if delay > MaxRetryDelay/2 {
			return MaxRetryDelay
		}
		delay *= 2
	}
	if delay > MaxRetryDelay {
		return MaxRetryDelay
	}
	return delay
}
