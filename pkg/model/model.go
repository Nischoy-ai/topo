// Package model defines Nischoy Topo's destination-neutral discovery contract.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "v1alpha1"

type AssetType string

const (
	AssetHost             AssetType = "host"
	AssetNetworkInterface AssetType = "network_interface"
	AssetVolume           AssetType = "volume"
	AssetSoftware         AssetType = "software"
	AssetService          AssetType = "service"
	AssetVirtualMachine   AssetType = "virtual_machine"
	AssetCloudResource    AssetType = "cloud_resource"
	AssetKubernetesObject AssetType = "kubernetes_object"
)

type Evidence struct {
	Source     string    `json:"source"`
	Collected  time.Time `json:"collected_at"`
	Path       string    `json:"path,omitempty"`
	Confidence float64   `json:"confidence"`
}

type Asset struct {
	Type        AssetType         `json:"type"`
	NativeID    string            `json:"native_id"`
	Name        string            `json:"name,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty"`
	Attributes  map[string]any    `json:"attributes,omitempty"`
	Evidence    []Evidence        `json:"evidence,omitempty"`
}

type Relationship struct {
	Type         string     `json:"type"`
	FromNativeID string     `json:"from_native_id"`
	ToNativeID   string     `json:"to_native_id"`
	Evidence     []Evidence `json:"evidence,omitempty"`
}

type CollectionError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type ObservationEnvelope struct {
	SchemaVersion string            `json:"schema_version"`
	ObservationID string            `json:"observation_id"`
	SiteID        string            `json:"site_id"`
	CollectorID   string            `json:"collector_id"`
	Plugin        string            `json:"plugin"`
	JobID         string            `json:"job_id,omitempty"`
	ObservedAt    time.Time         `json:"observed_at"`
	Assets        []Asset           `json:"assets"`
	Relationships []Relationship    `json:"relationships,omitempty"`
	Errors        []CollectionError `json:"errors,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// HeartbeatRequest is the POST /v1/heartbeats request body: a lightweight
// liveness signal, distinct from an observation delivery, so the
// controller can tell a collector is alive between discovery scans.
type HeartbeatRequest struct {
	SchemaVersion string `json:"schema_version"`
	CollectorID   string `json:"collector_id"`
	SiteID        string `json:"site_id"`
}

// CollectorStatus is one entry in the GET /v1/collectors response: a
// collector's most recently known liveness, derived from its heartbeats.
type CollectorStatus struct {
	CollectorID   string    `json:"collector_id"`
	SiteID        string    `json:"site_id"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Alive         bool      `json:"alive"`
}

// JobType enumerates the kinds of work a controller can dispatch to a
// collector. There is exactly one today, since it is the only capability
// `topo agent run` actually has: an out-of-schedule discovery pass.
type JobType string

const JobTypeDiscover JobType = "discover"

// Job is one unit of work a controller has queued for a specific
// collector, returned by GET /v1/jobs when that collector polls.
type Job struct {
	JobID       string    `json:"job_id"`
	CollectorID string    `json:"collector_id"`
	Type        JobType   `json:"type"`
	CreatedAt   time.Time `json:"created_at"`
}

// JobRequest is the POST /v1/jobs request body: an operator queuing one
// job for a specific collector.
type JobRequest struct {
	CollectorID string  `json:"collector_id"`
	Type        JobType `json:"type"`
}

// JobResult is the POST /v1/jobs/{id}/result request body: a collector
// reporting back that it executed a dispatched job. CollectorID is
// overridden by a verified mTLS peer certificate's identity when present,
// exactly like HeartbeatRequest.CollectorID.
type JobResult struct {
	CollectorID string    `json:"collector_id"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

// JobStatus is the GET /v1/jobs/{id} response: a job's current lifecycle
// state and, once known, its result.
type JobStatus struct {
	JobID       string    `json:"job_id"`
	CollectorID string    `json:"collector_id"`
	Type        JobType   `json:"type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at"`
	Error       string    `json:"error,omitempty"`
}

// StableAssetID produces a deterministic ID without making IP address an identity.
func StableAssetID(a Asset) string {
	parts := []string{string(a.Type), a.NativeID}
	keys := make([]string, 0, len(a.Identifiers))
	for k := range a.Identifiers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+a.Identifiers[k])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}
