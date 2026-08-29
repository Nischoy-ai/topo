package servicenow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/Nischoy-ai/topo/pkg/model"
)

const (
	MaxInputBytes  = 10 << 20
	MaxRecordBytes = 1 << 20
	MaxJSONDepth   = 64
	maxIdentityLen = 1024
	maxNameLen     = 1024
)

// DecodeJSONLines reads the destination-neutral observation format emitted by
// Topo discovery commands. The whole input, each record, nesting depth, and
// envelope count are bounded before any ServiceNow credential is needed.
func DecodeJSONLines(r io.Reader) ([]model.ObservationEnvelope, error) {
	if r == nil {
		return nil, errors.New("ServiceNow observation input is required")
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ServiceNow observation input: %w", err)
	}
	if len(raw) > MaxInputBytes {
		return nil, fmt.Errorf("ServiceNow observation input exceeds %d bytes", MaxInputBytes)
	}

	lines := bytes.Split(raw, []byte{'\n'})
	envelopes := make([]model.ObservationEnvelope, 0, len(lines))
	for lineNumber, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if len(line) > MaxRecordBytes {
			return nil, fmt.Errorf("ServiceNow observation input line %d exceeds %d bytes", lineNumber+1, MaxRecordBytes)
		}
		if len(envelopes) == MaxEnvelopes {
			return nil, fmt.Errorf("ServiceNow observation input exceeds %d envelopes", MaxEnvelopes)
		}
		if err := validateJSONDepth(line); err != nil {
			return nil, fmt.Errorf("ServiceNow observation input line %d: %w", lineNumber+1, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var envelope model.ObservationEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			return nil, fmt.Errorf("decode ServiceNow observation input line %d: %w", lineNumber+1, err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple JSON values")
			}
			return nil, fmt.Errorf("decode ServiceNow observation input line %d: %w", lineNumber+1, err)
		}
		envelopes = append(envelopes, envelope)
	}
	if err := validateEnvelopes(envelopes); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func validateJSONDepth(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			continue
		}
		switch delimiter {
		case '{', '[':
			depth++
			if depth > MaxJSONDepth {
				return fmt.Errorf("JSON nesting exceeds %d levels", MaxJSONDepth)
			}
		case '}', ']':
			depth--
		}
	}
}

func validateEnvelopes(envelopes []model.ObservationEnvelope) error {
	if len(envelopes) == 0 {
		return errors.New("ServiceNow observation input contains no envelopes")
	}
	if len(envelopes) > MaxEnvelopes {
		return fmt.Errorf("ServiceNow observation input exceeds %d envelopes", MaxEnvelopes)
	}

	assetTypes := make(map[string]model.AssetType)
	for envelopeIndex, envelope := range envelopes {
		if envelope.SchemaVersion != model.SchemaVersion {
			return fmt.Errorf("envelope %d has unsupported schema_version %q", envelopeIndex+1, envelope.SchemaVersion)
		}
		identities := []struct{ label, value string }{
			{"observation_id", envelope.ObservationID},
			{"site_id", envelope.SiteID},
			{"collector_id", envelope.CollectorID},
			{"plugin", envelope.Plugin},
		}
		for _, identity := range identities {
			if !validIdentity(identity.value) {
				return fmt.Errorf("envelope %d %s must be 1 to %d bytes and contain no control characters", envelopeIndex+1, identity.label, maxIdentityLen)
			}
		}
		if envelope.ObservedAt.IsZero() {
			return fmt.Errorf("envelope %d observed_at is required", envelopeIndex+1)
		}

		for assetIndex, asset := range envelope.Assets {
			if _, ok := classFor(asset.Type); !ok {
				return fmt.Errorf("envelope %d asset %d has unsupported ServiceNow asset type %q", envelopeIndex+1, assetIndex+1, asset.Type)
			}
			if !validIdentity(asset.NativeID) {
				return fmt.Errorf("envelope %d asset %d native_id must be 1 to %d bytes and contain no control characters", envelopeIndex+1, assetIndex+1, maxIdentityLen)
			}
			if len(asset.Name) > maxNameLen || strings.IndexFunc(asset.Name, unicode.IsControl) >= 0 {
				return fmt.Errorf("envelope %d asset %d name exceeds %d bytes or contains control characters", envelopeIndex+1, assetIndex+1, maxNameLen)
			}
			if asset.Type == model.AssetNetworkInterface {
				if value, exists := asset.Attributes["mac_address"]; exists {
					mac, ok := value.(string)
					if !ok || len(mac) > 128 || strings.IndexFunc(mac, unicode.IsControl) >= 0 {
						return fmt.Errorf("envelope %d asset %d mac_address must be a string of at most 128 bytes without control characters", envelopeIndex+1, assetIndex+1)
					}
				}
			}
			if previous, exists := assetTypes[asset.NativeID]; exists && previous != asset.Type {
				return fmt.Errorf("source_native_key %q changes ServiceNow class from %q to %q", asset.NativeID, previous, asset.Type)
			}
			assetTypes[asset.NativeID] = asset.Type
			if len(assetTypes) > MaxItems {
				return fmt.Errorf("ServiceNow IRE request exceeds %d unique items", MaxItems)
			}
		}
	}
	if len(assetTypes) == 0 {
		return errors.New("ServiceNow observation input contains no assets")
	}

	type relationKey struct{ typ, from, to string }
	seen := make(map[relationKey]struct{})
	for envelopeIndex, envelope := range envelopes {
		for relationIndex, relation := range envelope.Relationships {
			if _, ok := relationFor(relation.Type); !ok {
				return fmt.Errorf("envelope %d relationship %d has unsupported ServiceNow relationship type %q", envelopeIndex+1, relationIndex+1, relation.Type)
			}
			fromType, fromOK := assetTypes[relation.FromNativeID]
			toType, toOK := assetTypes[relation.ToNativeID]
			if !fromOK || !toOK {
				return fmt.Errorf("envelope %d relationship %d has an endpoint not present in the input", envelopeIndex+1, relationIndex+1)
			}
			if relation.Type == "host_has_interface" && (fromType != model.AssetHost || toType != model.AssetNetworkInterface) {
				return fmt.Errorf("envelope %d relationship %d host_has_interface requires host -> network_interface endpoints", envelopeIndex+1, relationIndex+1)
			}
			key := relationKey{relation.Type, relation.FromNativeID, relation.ToNativeID}
			seen[key] = struct{}{}
			if len(seen) > MaxRelations {
				return fmt.Errorf("ServiceNow IRE request exceeds %d unique relationships", MaxRelations)
			}
		}
	}
	return nil
}

func validIdentity(value string) bool {
	return value != "" && len(value) <= maxIdentityLen && strings.IndexFunc(value, unicode.IsControl) < 0
}
