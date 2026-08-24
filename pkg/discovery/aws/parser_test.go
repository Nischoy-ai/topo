package aws

import (
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestInventoryAssetsMapsHierarchyAndIdentity(t *testing.T) {
	joined := time.Unix(1700000000, 0).UTC()
	inv := Inventory{
		Organization: OrganizationInfo{ID: "o-example001", ManagementAccountID: "111111111111"},
		Roots:        []RootInfo{{ID: "r-topo", Name: "Root"}},
		OUs:          []OUInfo{{ID: "ou-topo-prod0001", Name: "Production", ParentID: "r-topo"}},
		Accounts: []AccountInfo{
			{ID: "100000000001", Name: "root-account", ParentID: "r-topo"},
			{ID: "100000000002", Name: "prod-account", ParentID: "ou-topo-prod0001", State: "ACTIVE", JoinedTimestamp: &joined},
		},
	}

	assets, relationships := inv.Assets(time.Now())
	if len(assets) != 5 { // organization + root + ou + 2 accounts
		t.Fatalf("got %d assets, want 5", len(assets))
	}
	for _, a := range assets {
		if a.Type != model.AssetCloudResource {
			t.Fatalf("asset %q has type %q, want %q", a.NativeID, a.Type, model.AssetCloudResource)
		}
	}

	byID := map[string]model.Asset{}
	for _, a := range assets {
		byID[a.NativeID] = a
	}
	if byID["o-example001"].Attributes["kind"] != "Organization" {
		t.Fatal("organization asset missing or mis-kinded")
	}
	if byID["r-topo"].Attributes["kind"] != "Root" {
		t.Fatal("root asset missing or mis-kinded")
	}
	if byID["ou-topo-prod0001"].Attributes["kind"] != "OrganizationalUnit" {
		t.Fatal("OU asset missing or mis-kinded")
	}
	account := byID["100000000002"]
	if account.Attributes["kind"] != "Account" || account.Name != "prod-account" {
		t.Fatalf("unexpected account asset: %#v", account)
	}
	if account.Attributes["joined_at"] != joined.Format(time.RFC3339) {
		t.Fatalf("unexpected joined_at: %#v", account.Attributes["joined_at"])
	}

	wantRelationships := map[string]string{ // FromNativeID -> ToNativeID
		"r-topo":           "o-example001",
		"ou-topo-prod0001": "r-topo",
		"100000000001":     "r-topo",
		"100000000002":     "ou-topo-prod0001",
	}
	if len(relationships) != len(wantRelationships) {
		t.Fatalf("got %d relationships, want %d", len(relationships), len(wantRelationships))
	}
	for _, r := range relationships {
		if r.Type != "member_of" {
			t.Fatalf("relationship %+v has type %q, want member_of", r, r.Type)
		}
		if want := wantRelationships[r.FromNativeID]; want != r.ToNativeID {
			t.Fatalf("relationship from %q: got parent %q, want %q", r.FromNativeID, r.ToNativeID, want)
		}
	}
}

func TestInventoryAssetsAccountIdentityIsAccountIDNotName(t *testing.T) {
	inv := Inventory{
		Organization: OrganizationInfo{ID: "o-example001"},
		Accounts:     []AccountInfo{{ID: "100000000009", Name: "renamed-later", ParentID: "o-example001"}},
	}
	assets, _ := inv.Assets(time.Now())
	for _, a := range assets {
		if a.Attributes["kind"] == "Account" && a.NativeID != "100000000009" {
			t.Fatalf("account identity should be the account ID, got NativeID %q", a.NativeID)
		}
	}
}
