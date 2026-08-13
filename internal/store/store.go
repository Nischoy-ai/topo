package store

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/Nischoy-ai/topo/pkg/model"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	SaveObservation(context.Context, model.ObservationEnvelope) error
	ListObservations(context.Context) ([]model.ObservationEnvelope, error)
	ListAssets(context.Context) ([]ResolvedAsset, error)
}
type ResolvedAsset struct {
	ID                 string      `json:"id"`
	Asset              model.Asset `json:"asset"`
	FirstObservationID string      `json:"first_observation_id"`
	LastObservationID  string      `json:"last_observation_id"`
}

type Memory struct {
	mu           sync.RWMutex
	observations []model.ObservationEnvelope
	assets       map[string]ResolvedAsset
}

func NewMemory() *Memory { return &Memory{assets: map[string]ResolvedAsset{}} }
func (m *Memory) SaveObservation(_ context.Context, e model.ObservationEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations = append(m.observations, e)
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
