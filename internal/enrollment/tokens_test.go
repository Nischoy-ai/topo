package enrollment

import (
	"errors"
	"testing"
	"time"
)

func TestTokenStoreIssueAndRedeem(t *testing.T) {
	store := NewTokenStore()
	token, expiresAt, err := store.Issue("collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expected expiresAt to be in the future")
	}
	if err := store.Redeem(token, "collector-1"); err != nil {
		t.Fatal(err)
	}
}

func TestTokenStoreRedeemIsSingleUse(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue("collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Redeem(token, "collector-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Redeem(token, "collector-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken on reuse", err)
	}
}

func TestTokenStoreRejectsUnknownToken(t *testing.T) {
	store := NewTokenStore()
	if err := store.Redeem("does-not-exist", "collector-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenStoreRejectsExpiredToken(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue("collector-1", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := store.Redeem(token, "collector-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenStoreIssueRejectsNonPositiveTTL(t *testing.T) {
	store := NewTokenStore()
	if _, _, err := store.Issue("collector-1", 0); err == nil {
		t.Fatal("expected a non-positive ttl to be rejected")
	}
}

func TestTokenStoreIssueRejectsInvalidCollectorID(t *testing.T) {
	store := NewTokenStore()
	if _, _, err := store.Issue("", time.Hour); err == nil {
		t.Fatal("expected an invalid collector ID to be rejected")
	}
}

func TestTokenStoreCollectorMismatchDoesNotConsumeToken(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue("collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Redeem(token, "collector-2"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("mismatch error = %v, want ErrInvalidToken", err)
	}
	if err := store.Redeem(token, "collector-1"); err != nil {
		t.Fatalf("redeem for bound collector after mismatch: %v", err)
	}
}

func TestTokenStoreConcurrentRedeemOnlySucceedsOnce(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue("collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 20
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() { results <- store.Redeem(token, "collector-1") }()
	}
	successes := 0
	for i := 0; i < attempts; i++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful redemptions = %d, want exactly 1", successes)
	}
}

func TestTokenStoreConcurrentCollectorMismatchCannotWin(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue("collector-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 20
	type result struct {
		collectorID string
		err         error
	}
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		collectorID := "collector-2"
		if i%2 == 0 {
			collectorID = "collector-1"
		}
		go func() { results <- result{collectorID: collectorID, err: store.Redeem(token, collectorID)} }()
	}
	successes := 0
	for i := 0; i < attempts; i++ {
		result := <-results
		if result.err == nil {
			successes++
			if result.collectorID != "collector-1" {
				t.Fatalf("token redeemed by mismatched collector %q", result.collectorID)
			}
		} else if !errors.Is(result.err, ErrInvalidToken) {
			t.Fatalf("error = %v, want ErrInvalidToken", result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful redemptions = %d, want exactly 1", successes)
	}
}
