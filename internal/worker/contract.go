// Package worker implements the outbound-only, stateless worker used by the
// ServiceNow-managed Topo control plane.
package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

const (
	ContractVersion  = "v1alpha1"
	OperationLocalV1 = "local.v1"
)

type RegisterRequest struct {
	SchemaVersion string    `json:"schema_version"`
	BootID        string    `json:"boot_id"`
	WorkerPool    string    `json:"worker_pool"`
	SiteID        string    `json:"site_id"`
	Version       string    `json:"version"`
	Capabilities  []string  `json:"capabilities"`
	PolicyDigest  string    `json:"policy_digest"`
	StartedAt     time.Time `json:"started_at"`
}

type RegisterResponse struct {
	WorkerID string `json:"worker_id"`
}

type HeartbeatRequest struct {
	SchemaVersion string    `json:"schema_version"`
	WorkerID      string    `json:"worker_id"`
	BootID        string    `json:"boot_id"`
	CurrentLeases int       `json:"current_leases"`
	SentAt        time.Time `json:"sent_at"`
}

type HeartbeatResponse struct {
	CancelAttemptIDs []string `json:"cancel_attempt_ids"`
}

type ClaimRequest struct {
	SchemaVersion string   `json:"schema_version"`
	WorkerID      string   `json:"worker_id"`
	BootID        string   `json:"boot_id"`
	Capabilities  []string `json:"capabilities"`
}

type Task struct {
	TaskID          string    `json:"task_id"`
	RunID           string    `json:"run_id"`
	AttemptID       string    `json:"attempt_id"`
	LeaseToken      string    `json:"lease_token"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
	Operation       string    `json:"operation"`
	ProfileID       string    `json:"profile_id"`
	ProfileRevision int       `json:"profile_revision"`
	Deadline        time.Time `json:"deadline"`
}

// UnmarshalJSON accepts ServiceNow's JSON representation of an integral
// Glide integer (for example 1.0) without weakening strict field validation.
func (t *Task) UnmarshalJSON(data []byte) error {
	type taskAlias Task
	var wire struct {
		taskAlias
		ProfileRevision json.Number `json:"profile_revision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return err
	}
	revision, err := wire.ProfileRevision.Float64()
	if err != nil || math.IsNaN(revision) || math.IsInf(revision, 0) || math.Trunc(revision) != revision || revision < math.MinInt || revision > math.MaxInt {
		return fmt.Errorf("profile_revision must be an integer")
	}
	*t = Task(wire.taskAlias)
	t.ProfileRevision = int(revision)
	return nil
}

type ClaimResponse struct {
	Task *Task `json:"task"`
}

type RenewRequest struct {
	SchemaVersion string `json:"schema_version"`
	WorkerID      string `json:"worker_id"`
	BootID        string `json:"boot_id"`
	AttemptID     string `json:"attempt_id"`
	LeaseToken    string `json:"lease_token"`
}

type RenewResponse struct {
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	Cancelled      bool      `json:"cancelled"`
}

type ResultChunkRequest struct {
	SchemaVersion   string `json:"schema_version"`
	WorkerID        string `json:"worker_id"`
	BootID          string `json:"boot_id"`
	AttemptID       string `json:"attempt_id"`
	LeaseToken      string `json:"lease_token"`
	ChunkNumber     int    `json:"chunk_number"`
	ChunkCount      int    `json:"chunk_count"`
	Checksum        string `json:"checksum_sha256"`
	ObservationJSON string `json:"observation_json"`
}

type ResultChunkResponse struct {
	Accepted  bool `json:"accepted"`
	Duplicate bool `json:"duplicate"`
}

type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type CompleteRequest struct {
	SchemaVersion string   `json:"schema_version"`
	WorkerID      string   `json:"worker_id"`
	BootID        string   `json:"boot_id"`
	AttemptID     string   `json:"attempt_id"`
	LeaseToken    string   `json:"lease_token"`
	Success       bool     `json:"success"`
	ChunkCount    int      `json:"chunk_count"`
	Failure       *Failure `json:"failure,omitempty"`
}

type CompleteResponse struct {
	TaskState string `json:"task_state"`
	RunState  string `json:"run_state"`
}
