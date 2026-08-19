package controller

import (
	"sort"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

// DefaultHeartbeatStaleAfter is how long since a collector's last
// heartbeat before GET /v1/collectors reports it as no longer alive. It is
// a fixed multiple of the agent's default -heartbeat-interval (one
// minute), not something set per collector, since the controller has no
// reliable way to know an individual collector's actual configured
// interval.
const DefaultHeartbeatStaleAfter = 3 * time.Minute

type heartbeatRecord struct {
	siteID   string
	lastSeen time.Time
}

// HeartbeatStore tracks each collector's most recent heartbeat. Like
// TokenStore, it is in-memory only and does not survive a controller
// restart — a collector's liveness is inherently transient, not durable
// domain data, so this deliberately does not go through store.Repository.
type HeartbeatStore struct {
	mu         sync.RWMutex
	records    map[string]*heartbeatRecord
	staleAfter time.Duration
}

// NewHeartbeatStore returns an empty HeartbeatStore. A collector's most
// recent heartbeat counts as stale once staleAfter has passed since it
// arrived.
func NewHeartbeatStore(staleAfter time.Duration) *HeartbeatStore {
	return &HeartbeatStore{records: map[string]*heartbeatRecord{}, staleAfter: staleAfter}
}

// Record stores collectorID's heartbeat as having just arrived, replacing
// whatever was previously recorded for it.
func (s *HeartbeatStore) Record(collectorID, siteID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[collectorID] = &heartbeatRecord{siteID: siteID, lastSeen: time.Now()}
}

// List returns every known collector's current status, ordered by
// collector ID.
func (s *HeartbeatStore) List() []model.CollectorStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]model.CollectorStatus, 0, len(s.records))
	for id, r := range s.records {
		out = append(out, model.CollectorStatus{
			CollectorID:   id,
			SiteID:        r.siteID,
			LastHeartbeat: r.lastSeen,
			Alive:         now.Sub(r.lastSeen) < s.staleAfter,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CollectorID < out[j].CollectorID })
	return out
}
