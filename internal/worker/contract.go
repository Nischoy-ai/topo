// Package worker implements the outbound-only, stateless worker used by the
// ServiceNow-managed Topo control plane.
package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"
)

const (
	ContractVersion     = "v1alpha1"
	OperationLocalV1    = "local.v1"
	OperationSSHLinuxV1 = "ssh_linux.v1"
	sha256HexLength     = 64
)

type RegisterRequest struct {
	SchemaVersion  string    `json:"schema_version"`
	BootID         string    `json:"boot_id"`
	WorkerPool     string    `json:"worker_pool"`
	SiteID         string    `json:"site_id"`
	Version        string    `json:"version"`
	Capabilities   []string  `json:"capabilities"`
	PolicyDigest   string    `json:"policy_digest"`
	MaxConcurrency int       `json:"max_concurrency"`
	StartedAt      time.Time `json:"started_at"`
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
	CurrentLeases int      `json:"current_leases"`
}

// TargetPartition is immutable application-owned selection metadata. Slice B
// plans canonical CIDR partitions for later reviewed operations; local.v1
// production tasks continue to omit it because local discovery has no remote
// target authority.
type TargetPartition struct {
	Key     string   `json:"key"`
	Ordinal int      `json:"ordinal"`
	Count   int      `json:"count"`
	CIDRs   []string `json:"cidrs"`
}

// UnmarshalJSON accepts ServiceNow's JSON representation of integral Glide
// integers (for example 0.0) while retaining strict object and value parsing.
func (p *TargetPartition) UnmarshalJSON(data []byte) error {
	type partitionAlias TargetPartition
	var wire struct {
		partitionAlias
		Ordinal json.Number `json:"ordinal"`
		Count   json.Number `json:"count"`
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
	ordinal, err := serviceNowInteger(wire.Ordinal, "target_partition.ordinal")
	if err != nil {
		return err
	}
	count, err := serviceNowInteger(wire.Count, "target_partition.count")
	if err != nil {
		return err
	}
	*p = TargetPartition(wire.partitionAlias)
	p.Ordinal = ordinal
	p.Count = count
	return nil
}

type Task struct {
	TaskID              string           `json:"task_id"`
	RunID               string           `json:"run_id"`
	AttemptID           string           `json:"attempt_id"`
	LeaseToken          string           `json:"lease_token"`
	LeaseExpiresAt      time.Time        `json:"lease_expires_at"`
	Operation           string           `json:"operation"`
	ProfileID           string           `json:"profile_id"`
	ProfileRevision     int              `json:"profile_revision"`
	CredentialBindingID string           `json:"credential_binding_id,omitempty"`
	TargetPartition     *TargetPartition `json:"target_partition,omitempty"`
	Deadline            time.Time        `json:"deadline"`
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
	revision, err := serviceNowInteger(wire.ProfileRevision, "profile_revision")
	if err != nil {
		return err
	}
	*t = Task(wire.taskAlias)
	t.ProfileRevision = revision
	return nil
}

func serviceNowInteger(value json.Number, field string) (int, error) {
	number, ok := new(big.Rat).SetString(value.String())
	if !ok || !number.IsInt() || !number.Num().IsInt64() {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	value64 := number.Num().Int64()
	integer := int(value64)
	if int64(integer) != value64 {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return integer, nil
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

type CredentialRequest struct {
	SchemaVersion string `json:"schema_version"`
	WorkerID      string `json:"worker_id"`
	BootID        string `json:"boot_id"`
	AttemptID     string `json:"attempt_id"`
	LeaseToken    string `json:"lease_token"`
}

// SSHCredential is returned only by the fixed, attempt-bound credential
// broker. It is retained in memory for one execution and must never be logged,
// persisted, copied into an observation, or included in an error.
type SSHCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
