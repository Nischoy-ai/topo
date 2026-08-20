// Package audit implements an append-only, hash-chained log of
// admin/security-relevant controller actions (enrollment token issuance,
// collector enrollment, certificate rotation, job creation). "Immutable"
// here means tamper-evident, not physically write-once: each entry's hash
// covers its own content and the previous entry's hash, so editing,
// reordering, or removing an entry after the fact changes every subsequent
// hash and is detectable by VerifyChain.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is what a caller supplies to record one audit-worthy action. Detail
// must never carry secret material (tokens, private keys, API keys) since
// entries are retained indefinitely and returned by GET /v1/audit.
type Event struct {
	Action string
	Actor  string
	Detail map[string]string
}

// Entry is one persisted, hash-chained audit log record. Sequence,
// RecordedAt, PrevHash, and Hash are filled in by the storage backend when
// the entry is appended (see store.Repository.AppendAuditEvent) — callers
// only ever construct an Event.
type Entry struct {
	Sequence   int64             `json:"sequence"`
	RecordedAt time.Time         `json:"recorded_at"`
	Action     string            `json:"action"`
	Actor      string            `json:"actor"`
	Detail     map[string]string `json:"detail,omitempty"`
	PrevHash   string            `json:"prev_hash"`
	Hash       string            `json:"hash"`
}

// Chain builds the next Entry in the hash chain: prevHash is the previous
// entry's Hash ("" for the first entry), sequence is this entry's 1-based
// position, and recordedAt is normally time.Now(). Callers (store backends)
// invoke this under whatever lock or transaction already serializes their
// own appends, so sequence values come out gap-free and prevHash always
// matches the entry actually preceding it.
func Chain(prevHash string, sequence int64, recordedAt time.Time, e Event) Entry {
	entry := Entry{
		Sequence:   sequence,
		RecordedAt: recordedAt,
		Action:     e.Action,
		Actor:      e.Actor,
		Detail:     e.Detail,
		PrevHash:   prevHash,
	}
	entry.Hash = hashEntry(entry)
	return entry
}

func hashEntry(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d\x00%s\x00%s\x00%s\x00", e.Sequence, e.RecordedAt.UTC().Format(time.RFC3339Nano), e.Action, e.Actor)
	keys := make([]string, 0, len(e.Detail))
	for k := range e.Detail {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\x00", k, e.Detail[k])
	}
	b.WriteString(e.PrevHash)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// VerifyChain recomputes every entry's hash from its content and confirms
// each entry's PrevHash matches the hash of the entry before it, proving
// the log has not been edited, reordered, or had an entry removed since it
// was written. entries must be in Sequence order starting at 1, as
// returned by store.Repository.ListAuditEntries.
func VerifyChain(entries []Entry) error {
	prevHash := ""
	for i, e := range entries {
		if e.Sequence != int64(i+1) {
			return fmt.Errorf("entry at index %d: expected sequence %d, got %d", i, i+1, e.Sequence)
		}
		if e.PrevHash != prevHash {
			return fmt.Errorf("entry %d: prev_hash does not match the preceding entry's hash", e.Sequence)
		}
		if want := hashEntry(e); want != e.Hash {
			return fmt.Errorf("entry %d: hash does not match its recorded content", e.Sequence)
		}
		prevHash = e.Hash
	}
	return nil
}
