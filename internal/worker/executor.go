package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	localdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/local"
	"github.com/Nischoy-ai/topo/pkg/model"
)

type Executor struct {
	Policy Policy
	Now    func() time.Time
}

func (e Executor) Execute(ctx context.Context, task Task) (model.ObservationEnvelope, error) {
	if err := e.Policy.Validate(); err != nil {
		return model.ObservationEnvelope{}, err
	}
	if err := validateTask(task); err != nil {
		return model.ObservationEnvelope{}, err
	}
	if task.Operation != OperationLocalV1 || !e.Policy.AllowLocal {
		return model.ObservationEnvelope{}, fmt.Errorf("operation %q is not allowed by local worker policy", task.Operation)
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	current := now().UTC()
	if !task.LeaseExpiresAt.After(current) {
		return model.ObservationEnvelope{}, errors.New("task lease has expired")
	}
	if !task.Deadline.After(current) {
		return model.ObservationEnvelope{}, errors.New("task deadline has expired")
	}
	deadline := current.Add(e.Policy.taskDuration())
	if task.Deadline.Before(deadline) {
		deadline = task.Deadline
	}
	if task.LeaseExpiresAt.Before(deadline) {
		deadline = task.LeaseExpiresAt
	}
	discoverCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	request := discovery.Request{
		JobID:       task.TaskID,
		SiteID:      e.Policy.SiteID,
		CollectorID: "worker-pool-" + e.Policy.WorkerPool,
		Targets:     []string{"local"},
		Deadline:    deadline,
	}
	plugin := localdiscovery.Plugin{}
	if err := plugin.ValidateConfiguration(discoverCtx, request); err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("validate local.v1: %w", err)
	}
	observation, err := plugin.Discover(discoverCtx, request)
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("execute local.v1: %w", err)
	}
	return observation, nil
}
