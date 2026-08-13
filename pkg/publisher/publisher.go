package publisher

import (
	"context"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type Result struct {
	Destination string         `json:"destination"`
	Published   int            `json:"published"`
	Rejected    int            `json:"rejected"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

type Publisher interface {
	Validate(context.Context) error
	Preview(context.Context, []model.ObservationEnvelope) (any, error)
	PublishBatch(context.Context, []model.ObservationEnvelope) (Result, error)
}
