package audit

import (
	"testing"
	"time"
)

func chainThree(t *testing.T) []Entry {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var entries []Entry
	prevHash := ""
	for i, action := range []string{"enrollment_token_issued", "collector_enrolled", "job_created"} {
		e := Chain(prevHash, int64(i+1), now.Add(time.Duration(i)*time.Minute), Event{
			Action: action,
			Actor:  "tester",
			Detail: map[string]string{"n": string(rune('a' + i))},
		})
		entries = append(entries, e)
		prevHash = e.Hash
	}
	return entries
}

func TestChainProducesSequentialLinkedEntries(t *testing.T) {
	entries := chainThree(t)
	for i, e := range entries {
		if e.Sequence != int64(i+1) {
			t.Fatalf("entry %d: got sequence %d, want %d", i, e.Sequence, i+1)
		}
		if e.Hash == "" {
			t.Fatalf("entry %d: hash is empty", i)
		}
	}
	if entries[0].PrevHash != "" {
		t.Fatalf("first entry PrevHash = %q, want empty", entries[0].PrevHash)
	}
	if entries[1].PrevHash != entries[0].Hash || entries[2].PrevHash != entries[1].Hash {
		t.Fatal("PrevHash does not chain to the preceding entry's Hash")
	}
}

func TestChainIsDeterministic(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e1 := Chain("prev", 5, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"x": "1", "y": "2"}})
	e2 := Chain("prev", 5, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"y": "2", "x": "1"}})
	if e1.Hash != e2.Hash {
		t.Fatalf("hash depends on map iteration order: %q vs %q", e1.Hash, e2.Hash)
	}
}

func TestChainDistinguishesContent(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := Chain("prev", 1, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"x": "1"}})
	variants := []Entry{
		Chain("prev", 1, now, Event{Action: "job_created", Actor: "b", Detail: map[string]string{"x": "1"}}),
		Chain("prev", 1, now, Event{Action: "rotated", Actor: "a", Detail: map[string]string{"x": "1"}}),
		Chain("prev", 1, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"x": "2"}}),
		Chain("different-prev", 1, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"x": "1"}}),
		Chain("prev", 2, now, Event{Action: "job_created", Actor: "a", Detail: map[string]string{"x": "1"}}),
	}
	for i, v := range variants {
		if v.Hash == base.Hash {
			t.Fatalf("variant %d produced the same hash as the base entry", i)
		}
	}
}

func TestVerifyChainAcceptsAnIntactChain(t *testing.T) {
	if err := VerifyChain(chainThree(t)); err != nil {
		t.Fatalf("VerifyChain on an intact chain: %v", err)
	}
}

func TestVerifyChainAcceptsEmptyChain(t *testing.T) {
	if err := VerifyChain(nil); err != nil {
		t.Fatalf("VerifyChain on empty chain: %v", err)
	}
}

func TestVerifyChainDetectsEditedEntry(t *testing.T) {
	entries := chainThree(t)
	entries[1].Actor = "attacker"
	if err := VerifyChain(entries); err == nil {
		t.Fatal("VerifyChain accepted a chain with an edited entry")
	}
}

func TestVerifyChainDetectsRemovedEntry(t *testing.T) {
	entries := chainThree(t)
	spliced := []Entry{entries[0], entries[2]}
	if err := VerifyChain(spliced); err == nil {
		t.Fatal("VerifyChain accepted a chain with a removed entry")
	}
}

func TestVerifyChainDetectsReorderedEntries(t *testing.T) {
	entries := chainThree(t)
	entries[0], entries[1] = entries[1], entries[0]
	if err := VerifyChain(entries); err == nil {
		t.Fatal("VerifyChain accepted a chain with reordered entries")
	}
}

func TestVerifyChainDetectsForgedHashWithConsistentPrevHash(t *testing.T) {
	// An attacker who edits an entry's content but also recomputes and
	// rewrites PrevHash on every following entry to keep them internally
	// consistent must still be caught, because the forged entry's own Hash
	// no longer matches its (edited) content.
	entries := chainThree(t)
	entries[1].Detail = map[string]string{"n": "tampered"}
	for i := 2; i < len(entries); i++ {
		entries[i].PrevHash = entries[i-1].Hash
	}
	if err := VerifyChain(entries); err == nil {
		t.Fatal("VerifyChain accepted a chain with a forged entry")
	}
}
