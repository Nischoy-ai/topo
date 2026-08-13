package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type Capability struct {
	Name                string            `json:"name"`
	Version             string            `json:"version"`
	AssetTypes          []model.AssetType `json:"asset_types"`
	RequiredPermissions []string          `json:"required_permissions,omitempty"`
}

type Request struct {
	JobID       string            `json:"job_id"`
	SiteID      string            `json:"site_id"`
	CollectorID string            `json:"collector_id"`
	Targets     []string          `json:"targets,omitempty"`
	Options     map[string]string `json:"options,omitempty"`
	Deadline    time.Time         `json:"deadline,omitempty"`
}

type Plugin interface {
	DescribeCapabilities(context.Context) Capability
	ValidateConfiguration(context.Context, Request) error
	CheckConnectivity(context.Context, Request) error
	Discover(context.Context, Request) (model.ObservationEnvelope, error)
}

var ErrUnsupportedTarget = errors.New("unsupported discovery target")
