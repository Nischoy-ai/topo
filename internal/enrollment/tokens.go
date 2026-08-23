package enrollment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrInvalidToken indicates an enrollment token is unknown, expired, already
// redeemed, or presented for a collector other than the one it was issued for.
// Tokens are single-use, so a token intercepted after its legitimate collector
// has already enrolled cannot be replayed.
var ErrInvalidToken = errors.New("enrollment token is invalid, expired, or already used")

const tokenBytes = 32 // 256 bits

type tokenRecord struct {
	collectorID string
	expiresAt   time.Time
	redeemed    bool
}

// TokenStore issues and redeems single-use, time-bounded enrollment
// tokens. It is in-memory only, like the rest of Topo's current storage:
// tokens do not survive a controller restart. Persistent storage is a
// later, separately scoped milestone; mint a fresh token if the controller
// restarts mid-enrollment.
type TokenStore struct {
	mu     sync.Mutex
	tokens map[string]*tokenRecord
}

// NewTokenStore returns an empty TokenStore.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: map[string]*tokenRecord{}}
}

// Issue mints a new random token bound to collectorID and valid for ttl.
func (s *TokenStore) Issue(collectorID string, ttl time.Duration) (string, time.Time, error) {
	if !ValidCollectorID(collectorID) {
		return "", time.Time{}, errors.New("collector ID is empty, too long, or contains control characters")
	}
	if ttl <= 0 {
		return "", time.Time{}, errors.New("token ttl must be positive")
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("generate enrollment token: %w", err)
	}
	token := hex.EncodeToString(buf)
	expiresAt := time.Now().Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = &tokenRecord{collectorID: collectorID, expiresAt: expiresAt}
	return token, expiresAt, nil
}

// Redeem consumes token if it is valid, unused, and bound to collectorID. A
// collector mismatch returns the same error as an unknown token and does not
// consume it, so an operator can correct a mistaken enrollment request without
// minting another token and callers cannot probe which identity a token names.
// It is safe for concurrent use: only one caller can successfully redeem a
// given token, even if two requests race to redeem it simultaneously.
func (s *TokenStore) Redeem(token, collectorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tokens[token]
	if !ok || record.redeemed || time.Now().After(record.expiresAt) || record.collectorID != collectorID {
		return ErrInvalidToken
	}
	record.redeemed = true
	return nil
}
