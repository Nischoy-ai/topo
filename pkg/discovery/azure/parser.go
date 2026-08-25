package azure

import (
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

// TenantInfo is the single Azure AD (Microsoft Entra ID) tenant a target
// describes. ID is its full ARM resource path ("/tenants/{tenantId}"),
// matching the real API's own ID field shape.
type TenantInfo struct {
	ID, DisplayName, DefaultDomain string
}

// ManagementGroupInfo is one management group. ARMID (the full ARM
// resource path, e.g. "/providers/Microsoft.Management/managementGroups/
// {groupId}") is identity; ShortID (the bare group name Azure assigns —
// by convention identical to the tenant's own GUID for the automatically
// created "Tenant Root Group") is kept as an attribute only, never as
// identity, since it can collide with the tenant's own ID and is not
// globally unique the way an ARM resource path is. ParentID is either the
// tenant's ARM ID (for the root management group) or another management
// group's ARM ID.
type ManagementGroupInfo struct {
	ARMID, ShortID, DisplayName, ParentID string
}

// SubscriptionInfo is one subscription. ARMID ("/subscriptions/{subId}")
// is identity, for the same reason as ManagementGroupInfo.ARMID — never
// the mutable DisplayName. ParentID is either the tenant's ARM ID (a
// subscription attached directly to the root group) or a management
// group's ARM ID.
type SubscriptionInfo struct {
	ARMID, ShortID, DisplayName, State, TenantID, ParentID string
}

type Inventory struct {
	Tenant           TenantInfo
	ManagementGroups []ManagementGroupInfo
	Subscriptions    []SubscriptionInfo
}

// enrich merges the flat Subscriptions-list response onto the
// tree-derived Subscriptions entries: it fills in DisplayName/State for
// entries the recursive management-group walk already found (that walk's
// per-node DisplayName is present but State is not), and appends any
// subscription the flat list reports that the tree walk did not surface
// at all — attached directly under the tenant as a conservative fallback,
// since its true parent is otherwise unknown.
func enrich(inv *Inventory, subscriptions []*armsubscriptions.Subscription) {
	byARMID := make(map[string]int, len(inv.Subscriptions))
	for i, s := range inv.Subscriptions {
		byARMID[s.ARMID] = i
	}
	for _, s := range subscriptions {
		armID := valueOfString(s.ID)
		if armID == "" {
			continue
		}
		state := string(valueOfSubscriptionState(s.State))
		if i, ok := byARMID[armID]; ok {
			inv.Subscriptions[i].DisplayName = valueOfString(s.DisplayName)
			inv.Subscriptions[i].State = state
			continue
		}
		inv.Subscriptions = append(inv.Subscriptions, SubscriptionInfo{
			ARMID: armID, ShortID: valueOfString(s.SubscriptionID), DisplayName: valueOfString(s.DisplayName),
			State: state, TenantID: valueOfString(s.TenantID), ParentID: inv.Tenant.ID,
		})
	}
}

func valueOfSubscriptionState(v *armsubscriptions.SubscriptionState) armsubscriptions.SubscriptionState {
	if v == nil {
		return ""
	}
	return *v
}

// Assets maps the collected tenant structure to Topo's canonical model.
// Every object kind maps to the single model.AssetCloudResource type,
// distinguished by Attributes["kind"] ("Tenant", "ManagementGroup", or
// "Subscription") rather than a dedicated AssetType per kind — the same
// generic-type-plus-kind choice already made for Kubernetes's Node/Pod
// objects and AWS's Organization/Root/OrganizationalUnit/Account objects.
// A single "member_of" relationship — the same relationship type AWS's
// hierarchy uses — connects every management group and subscription to
// its immediate parent, forming the tenant's containment hierarchy.
func (inv Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "azure-tenant", Collected: now, Confidence: 1}}
	var assets []model.Asset
	var relationships []model.Relationship

	tenant := inv.Tenant
	assets = append(assets, model.Asset{
		Type:     model.AssetCloudResource,
		NativeID: tenant.ID,
		Name:     valueOr(tenant.DisplayName, tenant.ID),
		Identifiers: map[string]string{
			"kind":      "Tenant",
			"tenant_id": tenant.ID,
		},
		Attributes: map[string]any{
			"kind":           "Tenant",
			"display_name":   tenant.DisplayName,
			"default_domain": tenant.DefaultDomain,
		},
		Evidence: ev,
	})

	for _, mg := range inv.ManagementGroups {
		assets = append(assets, model.Asset{
			Type:     model.AssetCloudResource,
			NativeID: mg.ARMID,
			Name:     valueOr(mg.DisplayName, mg.ShortID),
			Identifiers: map[string]string{
				"kind":                "ManagementGroup",
				"management_group_id": mg.ShortID,
			},
			Attributes: map[string]any{"kind": "ManagementGroup", "display_name": mg.DisplayName},
			Evidence:   ev,
		})
		relationships = append(relationships, model.Relationship{Type: "member_of", FromNativeID: mg.ARMID, ToNativeID: mg.ParentID, Evidence: ev})
	}

	for _, sub := range inv.Subscriptions {
		assets = append(assets, model.Asset{
			Type:     model.AssetCloudResource,
			NativeID: sub.ARMID,
			Name:     valueOr(sub.DisplayName, sub.ShortID),
			Identifiers: map[string]string{
				"kind":            "Subscription",
				"subscription_id": sub.ShortID,
			},
			Attributes: map[string]any{
				"kind": "Subscription", "display_name": sub.DisplayName,
				"state": sub.State, "tenant_id": sub.TenantID,
			},
			Evidence: ev,
		})
		relationships = append(relationships, model.Relationship{Type: "member_of", FromNativeID: sub.ARMID, ToNativeID: sub.ParentID, Evidence: ev})
	}

	return assets, relationships
}
