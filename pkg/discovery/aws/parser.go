package aws

import (
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

// OrganizationInfo is the single AWS Organization object a target
// describes. Its ID is stable for the life of the organization and is
// never reassigned, matching the project's standing identity invariant.
type OrganizationInfo struct {
	ID                     string
	ARN                    string
	FeatureSet             string
	ManagementAccountID    string
	ManagementAccountARN   string
	ManagementAccountEmail string
}

// RootInfo is one organization root. AWS Organizations currently exposes
// exactly one root per organization in practice, but the API itself
// returns a list, so Topo does not assume a single root.
type RootInfo struct {
	ID, Name, ARN string
}

// OUInfo is one organizational unit. ParentID is either a root ID or
// another OU's ID.
type OUInfo struct {
	ID, Name, ARN, Path, ParentID string
}

// AccountInfo is one member account. ID is the account's 12-digit AWS
// account ID — stable identity, never the mutable account Name. ParentID
// is either a root ID or an OU's ID: AWS Organizations attaches an account
// to exactly one container at a time.
type AccountInfo struct {
	ID, Name, ARN, Email, State, JoinedMethod, ParentID string
	JoinedTimestamp                                     *time.Time
}

type Inventory struct {
	Organization OrganizationInfo
	Roots        []RootInfo
	OUs          []OUInfo
	Accounts     []AccountInfo
}

// Assets maps the collected organization structure to Topo's canonical
// model. Every object kind maps to the single model.AssetCloudResource
// type, distinguished by Attributes["kind"] ("Organization", "Root",
// "OrganizationalUnit", or "Account") rather than a dedicated AssetType
// per kind — AWS, Azure, and Kubernetes each have far more object kinds
// than Topo has fixed asset types, so a generic type plus a kind attribute
// scales the way a per-kind constant would not, the same choice already
// made for Kubernetes's Node/Pod objects. A single "member_of" relationship
// type connects every child (root, OU, or account) to its immediate parent
// (the organization or a root or OU), forming the organization's
// containment hierarchy.
func (inv Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "aws-organizations", Collected: now, Confidence: 1}}
	var assets []model.Asset
	var relationships []model.Relationship

	org := inv.Organization
	assets = append(assets, model.Asset{
		Type:     model.AssetCloudResource,
		NativeID: org.ID,
		Name:     org.ID,
		Identifiers: map[string]string{
			"kind":            "Organization",
			"organization_id": org.ID,
		},
		Attributes: map[string]any{
			"kind":                     "Organization",
			"arn":                      org.ARN,
			"feature_set":              org.FeatureSet,
			"management_account_id":    org.ManagementAccountID,
			"management_account_arn":   org.ManagementAccountARN,
			"management_account_email": org.ManagementAccountEmail,
		},
		Evidence: ev,
	})

	for _, root := range inv.Roots {
		assets = append(assets, model.Asset{
			Type:     model.AssetCloudResource,
			NativeID: root.ID,
			Name:     root.Name,
			Identifiers: map[string]string{
				"kind":    "Root",
				"root_id": root.ID,
			},
			Attributes: map[string]any{"kind": "Root", "arn": root.ARN},
			Evidence:   ev,
		})
		relationships = append(relationships, model.Relationship{Type: "member_of", FromNativeID: root.ID, ToNativeID: org.ID, Evidence: ev})
	}

	for _, ou := range inv.OUs {
		assets = append(assets, model.Asset{
			Type:     model.AssetCloudResource,
			NativeID: ou.ID,
			Name:     ou.Name,
			Identifiers: map[string]string{
				"kind":  "OrganizationalUnit",
				"ou_id": ou.ID,
			},
			Attributes: map[string]any{"kind": "OrganizationalUnit", "arn": ou.ARN, "path": ou.Path},
			Evidence:   ev,
		})
		relationships = append(relationships, model.Relationship{Type: "member_of", FromNativeID: ou.ID, ToNativeID: ou.ParentID, Evidence: ev})
	}

	for _, account := range inv.Accounts {
		attrs := map[string]any{
			"kind":          "Account",
			"arn":           account.ARN,
			"email":         account.Email,
			"state":         account.State,
			"joined_method": account.JoinedMethod,
		}
		if account.JoinedTimestamp != nil {
			attrs["joined_at"] = account.JoinedTimestamp.UTC().Format(time.RFC3339)
		}
		assets = append(assets, model.Asset{
			Type:     model.AssetCloudResource,
			NativeID: account.ID,
			Name:     account.Name,
			Identifiers: map[string]string{
				"kind":       "Account",
				"account_id": account.ID,
			},
			Attributes: attrs,
			Evidence:   ev,
		})
		relationships = append(relationships, model.Relationship{Type: "member_of", FromNativeID: account.ID, ToNativeID: account.ParentID, Evidence: ev})
	}

	return assets, relationships
}
