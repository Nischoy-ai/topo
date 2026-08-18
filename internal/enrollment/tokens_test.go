package enrollment

import (
	"errors"
	"testing"
	"time"
)

func TestTokenStoreIssueAndRedeem(t *testing.T) {
	store := NewTokenStore()
	token, expiresAt, err := store.Issue(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expected expiresAt to be in the future")
	}
	if err := store.Redeem(token); err != nil {
		t.Fatal(err)
	}
}

func TestTokenStoreRedeemIsSingleUse(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Redeem(token); err != nil {
		t.Fatal(err)
	}
	if err := store.Redeem(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken on reuse", err)
	}
}

func TestTokenStoreRejectsUnknownToken(t *testing.T) {
	store := NewTokenStore()
	if err := store.Redeem("does-not-exist"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenStoreRejectsExpiredToken(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := store.Redeem(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
}

func TestTokenStoreIssueRejectsNonPositiveTTL(t *testing.T) {
	store := NewTokenStore()
	if _, _, err := store.Issue(0); err == nil {
		t.Fatal("expected a non-positive ttl to be rejected")
	}
}

func TestTokenStoreConcurrentRedeemOnlySucceedsOnce(t *testing.T) {
	store := NewTokenStore()
	token, _, err := store.Issue(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 20
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() { results <- store.Redeem(token) }()
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
