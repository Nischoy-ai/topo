// Package relay implements the outbound-only ServiceNow-controlled Topo
// Relay. ServiceNow dispatches only a compiled-in job type plus a locally
// configured profile ID; it never supplies targets, credentials, or commands.
package relay

import "time"

const (
	ContractVersion = "v1alpha1"
	JobTypeDiscover = "discover"
)

// ProfileCapability is the deliberately small part of local Relay
// configuration advertised to ServiceNow. Security-sensitive configuration
// stays on the Relay host.
type ProfileCapability struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
}

// PollRequest is both a liveness check-in and a request for at most one job.
type PollRequest struct {
	SchemaVersion string              `json:"schema_version"`
	RelayID       string              `json:"relay_id"`
	SiteID        string              `json:"site_id"`
	Version       string              `json:"version"`
	Profiles      []ProfileCapability `json:"profiles"`
	SentAt        time.Time           `json:"sent_at"`
}

// Job is the complete control-plane authority ServiceNow gets over a Relay.
// Targets, options, credential references, and executable text are
// intentionally absent.
type Job struct {
	JobID       string    `json:"job_id"`
	Type        string    `json:"type"`
	ProfileID   string    `json:"profile_id"`
	RequestedAt time.Time `json:"requested_at"`
}

type pollResponse struct {
	Jobs []Job `json:"jobs"`
}

// JobResult is written back to the ServiceNow job record after IRE accepts
// the observation, or after discovery/publication fails.
type JobResult struct {
	SchemaVersion    string    `json:"schema_version"`
	JobID            string    `json:"job_id"`
	RelayID          string    `json:"relay_id"`
	ProfileID        string    `json:"profile_id"`
	Success          bool      `json:"success"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	ObservationID    string    `json:"observation_id,omitempty"`
	Assets           int       `json:"assets,omitempty"`
	Relationships    int       `json:"relationships,omitempty"`
	CollectionErrors int       `json:"collection_errors,omitempty"`
	Error            string    `json:"error,omitempty"`
}
