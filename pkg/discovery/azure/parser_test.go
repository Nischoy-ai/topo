package azure

import (
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

func TestInventoryAssetsMapsHierarchyAndIdentity(t *testing.T) {
	tenantID := "/tenants/11111111-1111-1111-1111-111111111111"
	rootID := "/providers/Microsoft.Management/managementGroups/11111111-1111-1111-1111-111111111111"
	prodID := "/providers/Microsoft.Management/managementGroups/topo-lab-prod"
	inv := Inventory{
		Tenant:           TenantInfo{ID: tenantID, DisplayName: "Topo Lab Tenant"},
		ManagementGroups: []ManagementGroupInfo{{ARMID: rootID, ShortID: "11111111-1111-1111-1111-111111111111", DisplayName: "Tenant Root Group", ParentID: tenantID}, {ARMID: prodID, ShortID: "topo-lab-prod", DisplayName: "Production", ParentID: rootID}},
		Subscriptions: []SubscriptionInfo{
			{ARMID: "/subscriptions/33333333-3333-3333-3333-333333333333", ShortID: "33333333-3333-3333-3333-333333333333", DisplayName: "root-sub", ParentID: rootID},
			{ARMID: "/subscriptions/44444444-4444-4444-4444-444444444444", ShortID: "44444444-4444-4444-4444-444444444444", DisplayName: "prod-sub", State: "Enabled", ParentID: prodID},
		},
	}

	assets, relationships := inv.Assets(time.Now())
	if len(assets) != 5 { // tenant + 2 management groups + 2 subscriptions
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
	if byID[tenantID].Attributes["kind"] != "Tenant" {
		t.Fatal("tenant asset missing or mis-kinded")
	}
	if byID[rootID].Attributes["kind"] != "ManagementGroup" {
		t.Fatal("root management group asset missing or mis-kinded")
	}
	if byID[tenantID].NativeID == byID[rootID].NativeID {
		t.Fatal("tenant and root management group must not share a NativeID even though Azure gives the root group the tenant's own GUID as its short name")
	}
	prodSub := byID["/subscriptions/44444444-4444-4444-4444-444444444444"]
	if prodSub.Attributes["kind"] != "Subscription" || prodSub.Name != "prod-sub" || prodSub.Attributes["state"] != "Enabled" {
		t.Fatalf("unexpected subscription asset: %#v", prodSub)
	}

	wantRelationships := map[string]string{ // FromNativeID -> ToNativeID
		rootID: tenantID,
		prodID: rootID,
		"/subscriptions/33333333-3333-3333-3333-333333333333": rootID,
		"/subscriptions/44444444-4444-4444-4444-444444444444": prodID,
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

func TestInventoryAssetsSubscriptionIdentityIsARMIDNotDisplayName(t *testing.T) {
	inv := Inventory{
		Tenant:        TenantInfo{ID: "/tenants/x"},
		Subscriptions: []SubscriptionInfo{{ARMID: "/subscriptions/99999999-9999-9999-9999-999999999999", ShortID: "99999999-9999-9999-9999-999999999999", DisplayName: "renamed-later", ParentID: "/tenants/x"}},
	}
	assets, _ := inv.Assets(time.Now())
	for _, a := range assets {
		if a.Attributes["kind"] == "Subscription" && a.NativeID != "/subscriptions/99999999-9999-9999-9999-999999999999" {
			t.Fatalf("subscription identity should be the ARM ID, got NativeID %q", a.NativeID)
		}
	}
}

func TestEnrichMergesFlatSubscriptionListOntoTreeEntries(t *testing.T) {
	inv := &Inventory{
		Tenant:        TenantInfo{ID: "/tenants/x"},
		Subscriptions: []SubscriptionInfo{{ARMID: "/subscriptions/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ShortID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", DisplayName: "tree-name", ParentID: "/tenants/x"}},
	}
	state := armsubscriptions.SubscriptionStateEnabled
	enrich(inv, []*armsubscriptions.Subscription{{
		ID: to.Ptr("/subscriptions/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), SubscriptionID: to.Ptr("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		DisplayName: to.Ptr("flat-list-name"), State: &state,
	}})
	if len(inv.Subscriptions) != 1 {
		t.Fatalf("enrich should merge into the existing entry, not append a duplicate; got %d entries", len(inv.Subscriptions))
	}
	if inv.Subscriptions[0].DisplayName != "flat-list-name" || inv.Subscriptions[0].State != "Enabled" {
		t.Fatalf("enrich did not merge flat-list fields: %#v", inv.Subscriptions[0])
	}
}
