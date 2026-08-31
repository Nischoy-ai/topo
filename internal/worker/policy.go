package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const DefaultMaxTaskDuration = 30 * time.Second

const (
	DefaultMaxConcurrency = 1
	MaxWorkerConcurrency  = 32
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Policy is read-only deployment configuration. The worker never rewrites it
// or treats ServiceNow configuration as authority to expand it.
type Policy struct {
	WorkerPool      string
	SiteID          string
	AllowLocal      bool
	MaxTaskDuration time.Duration
	MaxConcurrency  int
}

func (p Policy) Validate() error {
	if !safeID.MatchString(p.WorkerPool) {
		return errors.New("worker pool must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if !safeID.MatchString(p.SiteID) {
		return errors.New("site ID must use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if !p.AllowLocal {
		return errors.New("Slice A worker policy must explicitly allow local.v1")
	}
	if p.MaxTaskDuration == 0 {
		p.MaxTaskDuration = DefaultMaxTaskDuration
	}
	if p.MaxTaskDuration < time.Second || p.MaxTaskDuration > 10*time.Minute {
		return errors.New("maximum task duration must be between 1s and 10m")
	}
	if p.concurrency() < 1 || p.concurrency() > MaxWorkerConcurrency {
		return fmt.Errorf("maximum worker concurrency must be between 1 and %d", MaxWorkerConcurrency)
	}
	return nil
}

func (p Policy) concurrency() int {
	if p.MaxConcurrency == 0 {
		return DefaultMaxConcurrency
	}
	return p.MaxConcurrency
}

func (p Policy) taskDuration() time.Duration {
	if p.MaxTaskDuration == 0 {
		return DefaultMaxTaskDuration
	}
	return p.MaxTaskDuration
}

func (p Policy) Capabilities() []string {
	if !p.AllowLocal {
		return nil
	}
	return []string{OperationLocalV1}
}

func (p Policy) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion  string   `json:"schema_version"`
		WorkerPool     string   `json:"worker_pool"`
		SiteID         string   `json:"site_id"`
		Operations     []string `json:"operations"`
		MaxTaskSeconds int64    `json:"max_task_seconds"`
		MaxConcurrency int      `json:"max_concurrency"`
	}{
		SchemaVersion:  ContractVersion,
		WorkerPool:     p.WorkerPool,
		SiteID:         p.SiteID,
		Operations:     p.Capabilities(),
		MaxTaskSeconds: int64(p.taskDuration() / time.Second),
		MaxConcurrency: p.concurrency(),
	})
	if err != nil {
		return "", fmt.Errorf("encode worker policy: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
