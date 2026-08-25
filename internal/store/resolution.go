package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Nischoy-ai/topo/pkg/model"
)

const (
	maxSourcePrecedenceEntries = 64
	maxPluginNameBytes         = 128
)

// ValidateSourcePrecedence validates the ordered discovery-plugin names used
// by topo serve. Plugin names remain open to future compiled-in plugins, so
// validation constrains shape and size rather than maintaining a second list
// of today's implementations.
func ValidateSourcePrecedence(plugins []string) error {
	if len(plugins) > maxSourcePrecedenceEntries {
		return fmt.Errorf("source precedence contains %d plugins, maximum is %d", len(plugins), maxSourcePrecedenceEntries)
	}
	seen := make(map[string]struct{}, len(plugins))
	for _, plugin := range plugins {
		if plugin == "" {
			return fmt.Errorf("source precedence contains an empty plugin name")
		}
		if len(plugin) > maxPluginNameBytes {
			return fmt.Errorf("source precedence plugin %q exceeds %d bytes", plugin, maxPluginNameBytes)
		}
		for _, r := range plugin {
			if unicode.IsControl(r) || unicode.IsSpace(r) || r == ',' {
				return fmt.Errorf("source precedence plugin %q contains whitespace, a comma, or a control character", plugin)
			}
		}
		if _, ok := seen[plugin]; ok {
			return fmt.Errorf("source precedence plugin %q is repeated", plugin)
		}
		seen[plugin] = struct{}{}
	}
	return nil
}

// ResolveAssetClaims applies plugin precedence to source-preserving asset
// claims. A lower configured rank wins. Unlisted plugins share the rank after
// every configured plugin, then latest observation time and stable source
// identity break ties deterministically.
func ResolveAssetClaims(claims []AssetClaim, precedence []string) []ResolvedAsset {
	ranks := make(map[string]int, len(precedence))
	for i, plugin := range precedence {
		ranks[plugin] = i
	}
	byAsset := make(map[string][]AssetClaim)
	for _, claim := range claims {
		rank, explicit := ranks[claim.Source.Plugin]
		if !explicit {
			rank = len(precedence)
		}
		claim.Source.PrecedenceRank = rank
		claim.Source.ExplicitPrecedence = explicit
		byAsset[claim.AssetID] = append(byAsset[claim.AssetID], claim)
	}

	assetIDs := make([]string, 0, len(byAsset))
	for id := range byAsset {
		assetIDs = append(assetIDs, id)
	}
	sort.Strings(assetIDs)

	out := make([]ResolvedAsset, 0, len(assetIDs))
	for _, id := range assetIDs {
		assetClaims := byAsset[id]
		sort.Slice(assetClaims, func(i, j int) bool { return claimPrecedes(assetClaims[i], assetClaims[j]) })
		winner := assetClaims[0]
		resolved := ResolvedAsset{
			ID:                 id,
			Asset:              winner.Asset,
			FirstObservationID: winner.Source.FirstObservationID,
			LastObservationID:  winner.Source.LastObservationID,
			FirstObservedAt:    winner.Source.FirstObservedAt,
			LastObservedAt:     winner.Source.LastObservedAt,
			WinningSource:      winner.Source,
			Sources:            make([]AssetSource, 0, len(assetClaims)),
		}
		for _, claim := range assetClaims {
			resolved.Sources = append(resolved.Sources, claim.Source)
			if claim.Source.FirstObservedAt.Before(resolved.FirstObservedAt) {
				resolved.FirstObservedAt = claim.Source.FirstObservedAt
				resolved.FirstObservationID = claim.Source.FirstObservationID
			}
			if claim.Source.LastObservedAt.After(resolved.LastObservedAt) {
				resolved.LastObservedAt = claim.Source.LastObservedAt
				resolved.LastObservationID = claim.Source.LastObservationID
			}
		}
		resolved.Conflicts = assetConflicts(assetClaims)
		out = append(out, resolved)
	}
	return out
}

func claimPrecedes(a, b AssetClaim) bool {
	if a.Source.PrecedenceRank != b.Source.PrecedenceRank {
		return a.Source.PrecedenceRank < b.Source.PrecedenceRank
	}
	if !a.Source.LastObservedAt.Equal(b.Source.LastObservedAt) {
		return a.Source.LastObservedAt.After(b.Source.LastObservedAt)
	}
	return AssetSourceKey(a.Source.SiteID, a.Source.CollectorID, a.Source.Plugin) <
		AssetSourceKey(b.Source.SiteID, b.Source.CollectorID, b.Source.Plugin)
}

// AssetSourceKey returns the unambiguous storage key for one source. Hashing a
// JSON tuple avoids delimiter ambiguity even for observations accepted through
// the bearer-key compatibility path, whose legacy site/plugin fields are not
// constrained to the enrollment collector-ID grammar.
func AssetSourceKey(siteID, collectorID, plugin string) string {
	encoded, _ := json.Marshal([3]string{siteID, collectorID, plugin})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func assetConflicts(claims []AssetClaim) []AssetConflict {
	if len(claims) < 2 {
		return nil
	}
	fields := []string{"name"}
	identifierKeys := map[string]struct{}{}
	attributeKeys := map[string]struct{}{}
	for _, claim := range claims {
		for key := range claim.Asset.Identifiers {
			identifierKeys[key] = struct{}{}
		}
		for key := range claim.Asset.Attributes {
			attributeKeys[key] = struct{}{}
		}
	}
	fields = append(fields, prefixedSortedKeys("identifiers.", identifierKeys)...)
	fields = append(fields, prefixedSortedKeys("attributes.", attributeKeys)...)

	var conflicts []AssetConflict
	for _, field := range fields {
		values := make(map[string]struct{})
		conflictClaims := make([]AssetConflictClaim, 0, len(claims))
		for _, claim := range claims {
			value, present := assetField(claim.Asset, field)
			encoded, _ := json.Marshal(struct {
				Present bool `json:"present"`
				Value   any  `json:"value"`
			}{Present: present, Value: value})
			values[string(encoded)] = struct{}{}
			conflictClaims = append(conflictClaims, AssetConflictClaim{Source: claim.Source, Present: present, Value: value})
		}
		if len(values) > 1 {
			conflicts = append(conflicts, AssetConflict{Field: field, Claims: conflictClaims})
		}
	}
	return conflicts
}

func prefixedSortedKeys(prefix string, keys map[string]struct{}) []string {
	values := make([]string, 0, len(keys))
	for key := range keys {
		values = append(values, prefix+key)
	}
	sort.Strings(values)
	return values
}

func assetField(asset model.Asset, field string) (any, bool) {
	if field == "name" {
		return asset.Name, true
	}
	if key, ok := strings.CutPrefix(field, "identifiers."); ok {
		value, present := asset.Identifiers[key]
		return value, present
	}
	key, _ := strings.CutPrefix(field, "attributes.")
	value, present := asset.Attributes[key]
	return value, present
}
