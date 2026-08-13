package jsonlines

import (
	"context"
	"encoding/json"
	"io"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
)

type Publisher struct{ Writer io.Writer }

func (p Publisher) Validate(context.Context) error {
	if p.Writer == nil {
		return io.ErrClosedPipe
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
	enc := json.NewEncoder(p.Writer)
	for i, e := range envelopes {
		select {
		case <-ctx.Done():
			return publisher.Result{Destination: "jsonlines", Published: i}, ctx.Err()
		default:
		}
		if err := enc.Encode(e); err != nil {
			return publisher.Result{Destination: "jsonlines", Published: i, Rejected: len(envelopes) - i}, err
		}
	}
	return publisher.Result{Destination: "jsonlines", Published: len(envelopes)}, nil
}
