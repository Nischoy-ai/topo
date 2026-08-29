package irepublish

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

type testPublisher struct {
	results []publisher.Result
	errors  []error
	calls   int
}

func (p *testPublisher) PublishBatch(context.Context, []model.ObservationEnvelope) (publisher.Result, error) {
	index := p.calls
	p.calls++
	if index >= len(p.errors) {
		index = len(p.errors) - 1
	}
	return p.results[index], p.errors[index]
}

type classifiedError struct {
	retryable bool
}

func (e classifiedError) Error() string   { return "classified failure" }
func (e classifiedError) Retryable() bool { return e.retryable }

func TestPublishRetriesOnlyClassifiedFailures(t *testing.T) {
	p := &testPublisher{
		results: []publisher.Result{{Rejected: 1}, {Rejected: 1}, {Published: 1}},
		errors:  []error{classifiedError{retryable: true}, classifiedError{retryable: true}, nil},
	}
	status, err := Publish(t.Context(), p, []model.ObservationEnvelope{{}}, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 || status.Attempts != 3 || status.Result.Published != 1 || status.Failure != nil {
		t.Fatalf("calls=%d status=%#v", p.calls, status)
	}
}

func TestPublishDoesNotRetryPermanentFailure(t *testing.T) {
	p := &testPublisher{results: []publisher.Result{{Rejected: 1}}, errors: []error{classifiedError{retryable: false}}}
	status, err := Publish(t.Context(), p, nil, 5, 0)
	if err == nil || p.calls != 1 || status.Attempts != 1 || status.Failure == nil || status.Failure.Retryable {
		t.Fatalf("calls=%d status=%#v err=%v", p.calls, status, err)
	}
}

func TestPublishReportsRetryExhaustion(t *testing.T) {
	p := &testPublisher{results: []publisher.Result{{Rejected: 1}}, errors: []error{classifiedError{retryable: true}}}
	status, err := Publish(t.Context(), p, nil, 2, 0)
	if err == nil || p.calls != 2 || !status.Exhausted || status.Failure == nil || !status.Failure.Retryable {
		t.Fatalf("calls=%d status=%#v err=%v", p.calls, status, err)
	}
}

func TestPublishCancellationStopsBackoff(t *testing.T) {
	p := &testPublisher{results: []publisher.Result{{}}, errors: []error{classifiedError{retryable: true}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, err := Publish(ctx, p, nil, 3, time.Second)
	if !errors.Is(err, context.Canceled) || p.calls != 1 || status.Failure == nil || status.Failure.Retryable {
		t.Fatalf("calls=%d status=%#v err=%v", p.calls, status, err)
	}
}

func TestPublishValidatesRetryBounds(t *testing.T) {
	p := &testPublisher{results: []publisher.Result{{}}, errors: []error{nil}}
	for _, test := range []struct {
		attempts int
		delay    time.Duration
	}{{0, 0}, {MaxAttempts + 1, 0}, {1, -1}, {1, MaxRetryDelay + time.Second}} {
		if _, err := Publish(t.Context(), p, nil, test.attempts, test.delay); err == nil {
			t.Fatalf("Publish accepted attempts=%d delay=%s", test.attempts, test.delay)
		}
	}
}
