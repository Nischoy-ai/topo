package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/pkg/model"
)

var ErrNotFound = errors.New("not found")

// Repository is the storage abstraction every controller backend
// implements — today Memory (in-memory only, the default) and
// internal/store/sqlite.Store (persistent, opt-in via `topo serve
// -db-driver sqlite`). Both must produce identical observable behavior;
// see internal/store/storetest for the shared black-box test suite that
// enforces this.
type Repository interface {
	SaveObservation(context.Context, model.ObservationEnvelope) error
	ListObservations(context.Context) ([]model.ObservationEnvelope, error)
	ListAssets(context.Context) ([]ResolvedAsset, error)
	ListRelationships(context.Context) ([]ResolvedRelationship, error)

	// AppendAuditEvent records one admin/security-relevant controller
	// action, assigning it the next Sequence number and hash-chaining it
	// to the previously appended entry (see internal/audit). Implementations
	// must serialize appends against each other so Sequence values are
	// gap-free and PrevHash always references the entry actually preceding
	// it, even under concurrent callers.
	AppendAuditEvent(context.Context, audit.Event) (audit.Entry, error)
	// ListAuditEntries returns every audit entry in Sequence order, suitable
	// for passing directly to audit.VerifyChain.
	ListAuditEntries(context.Context) ([]audit.Entry, error)
}
type ResolvedAsset struct {
	ID                 string      `json:"id"`
	Asset              model.Asset `json:"asset"`
	FirstObservationID string      `json:"first_observation_id"`
	LastObservationID  string      `json:"last_observation_id"`
}

// ResolvedRelationship is a relationship's current state resolved across
// every observation that has reported it, the relationship counterpart to
// ResolvedAsset. FromNativeID/ToNativeID reference assets by NativeID, not
// by ResolvedAsset.ID, since a relationship's endpoints are reported in
// terms of the same NativeID the discovery plugin already used to build
// the Asset.
type ResolvedRelationship struct {
	ID                 string             `json:"id"`
	Relationship       model.Relationship `json:"relationship"`
	FirstObservationID string             `json:"first_observation_id"`
	LastObservationID  string             `json:"last_observation_id"`
}

type Memory struct {
	mu               sync.RWMutex
	observations     []model.ObservationEnvelope
	observationIndex map[string]int
	assets           map[string]ResolvedAsset
	relationships    map[string]ResolvedRelationship
	auditEntries     []audit.Entry
}

func NewMemory() *Memory {
	return &Memory{
		observationIndex: map[string]int{},
		assets:           map[string]ResolvedAsset{},
		relationships:    map[string]ResolvedRelationship{},
	}
}

// SaveObservation is idempotent by ObservationID: resubmitting the same ID
// (a collector retrying a delivery whose response was lost, for example)
// replaces that observation in place rather than appending a duplicate,
// matching the same idempotent-upsert behavior the SQLite backend uses.
func (m *Memory) SaveObservation(_ context.Context, e model.ObservationEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx, ok := m.observationIndex[e.ObservationID]; ok {
		m.observations[idx] = e
	} else {
		m.observationIndex[e.ObservationID] = len(m.observations)
		m.observations = append(m.observations, e)
	}
	for _, a := range e.Assets {
		id := model.StableAssetID(a)
		r, ok := m.assets[id]
		if !ok {
			r = ResolvedAsset{ID: id, FirstObservationID: e.ObservationID}
		}
		r.Asset = a
		r.LastObservationID = e.ObservationID
		m.assets[id] = r
	}
	for _, rel := range e.Relationships {
		id := model.StableRelationshipID(rel)
		r, ok := m.relationships[id]
		if !ok {
			r = ResolvedRelationship{ID: id, FirstObservationID: e.ObservationID}
		}
		r.Relationship = rel
		r.LastObservationID = e.ObservationID
		m.relationships[id] = r
	}
	return nil
}
func (m *Memory) ListObservations(_ context.Context) ([]model.ObservationEnvelope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]model.ObservationEnvelope(nil), m.observations...)
	return out, nil
}
func (m *Memory) ListAssets(_ context.Context) ([]ResolvedAsset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ResolvedAsset, 0, len(m.assets))
	for _, a := range m.assets {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m *Memory) ListRelationships(_ context.Context) ([]ResolvedRelationship, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ResolvedRelationship, 0, len(m.relationships))
	for _, r := range m.relationships {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// AppendAuditEvent is serialized under the same mutex as SaveObservation,
// so concurrent callers can never observe or produce a torn Sequence/PrevHash
// pair.
func (m *Memory) AppendAuditEvent(_ context.Context, e audit.Event) (audit.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prevHash := ""
	if n := len(m.auditEntries); n > 0 {
		prevHash = m.auditEntries[n-1].Hash
	}
	entry := audit.Chain(prevHash, int64(len(m.auditEntries)+1), time.Now(), e)
	m.auditEntries = append(m.auditEntries, entry)
	return entry, nil
}
func (m *Memory) ListAuditEntries(_ context.Context) ([]audit.Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]audit.Entry(nil), m.auditEntries...)
	return out, nil
}
