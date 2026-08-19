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
