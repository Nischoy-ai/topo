package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestValidateSourcePrecedence(t *testing.T) {
	if err := store.ValidateSourcePrecedence([]string{"vmware", "ssh-linux", "aws-organizations"}); err != nil {
		t.Fatalf("valid precedence rejected: %v", err)
	}
	for _, plugins := range [][]string{
		{""},
		{"vmware", "vmware"},
		{"bad plugin"},
		{"bad,plugin"},
		{strings.Repeat("x", 129)},
	} {
		if err := store.ValidateSourcePrecedence(plugins); err == nil {
			t.Fatalf("invalid precedence accepted: %#v", plugins)
		}
	}
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = "plugin-" + string(rune('a'+i))
	}
	if err := store.ValidateSourcePrecedence(tooMany); err == nil {
		t.Fatal("65 source precedence entries accepted")
	}
}

func TestResolveAssetClaimsBreaksEqualFreshnessTiesByStableSourceIdentity(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	claim := func(collector, name string) store.AssetClaim {
		return store.AssetClaim{
			AssetID: "same-id",
			Asset:   model.Asset{Type: model.AssetHost, NativeID: "host-1", Name: name},
			Source: store.AssetSource{
				SiteID: "site", CollectorID: collector, Plugin: "same-plugin",
				FirstObservationID: collector, LastObservationID: collector,
				FirstObservedAt: now, LastObservedAt: now,
			},
		}
	}
	claims := []store.AssetClaim{claim("collector-b", "b"), claim("collector-a", "a")}
	first := store.ResolveAssetClaims(claims, nil)
	second := store.ResolveAssetClaims([]store.AssetClaim{claims[1], claims[0]}, nil)
	if len(first) != 1 || len(second) != 1 || first[0].WinningSource.CollectorID != second[0].WinningSource.CollectorID {
		t.Fatalf("input order changed tie winner: %#v vs %#v", first, second)
	}
	want := "collector-a"
	if store.AssetSourceKey("site", "collector-b", "same-plugin") < store.AssetSourceKey("site", "collector-a", "same-plugin") {
		want = "collector-b"
	}
	if first[0].WinningSource.CollectorID != want {
		t.Fatalf("winner = %q, want stable-key winner %q", first[0].WinningSource.CollectorID, want)
	}
}
