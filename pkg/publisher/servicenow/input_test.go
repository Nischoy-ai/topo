package servicenow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestDecodeJSONLinesAcceptsBoundedObservations(t *testing.T) {
	first := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "host-1", Name: "host"})
	second := testEnvelope(model.Asset{Type: model.AssetNetworkInterface, NativeID: "nic-1", Name: "eth0", Attributes: map[string]any{"mac_address": "00:11:22:33:44:55"}})
	var input bytes.Buffer
	for _, envelope := range []model.ObservationEnvelope{first, second} {
		if err := json.NewEncoder(&input).Encode(envelope); err != nil {
			t.Fatal(err)
		}
	}
	envelopes, err := DecodeJSONLines(&input)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != 2 {
		t.Fatalf("decoded %d envelopes, want 2", len(envelopes))
	}
}

func TestDeveloperInstanceValidationFixtureCoversReviewedBoundary(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "examples", "servicenow", "ire-validation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	envelopes, err := DecodeJSONLines(file)
	if err != nil {
		t.Fatal(err)
	}
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	preview, err := p.Preview(t.Context(), envelopes)
	if err != nil {
		t.Fatal(err)
	}
	payload := preview.(IREPayload)
	if len(payload.Items) != 3 || len(payload.Relations) != 2 {
		t.Fatalf("validation fixture mapped to %d items and %d relationships", len(payload.Items), len(payload.Relations))
	}
}

func TestDecodeJSONLinesRejectsMalformedAndUnsupportedInput(t *testing.T) {
	valid := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "host-1"})
	validJSON, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"empty":                "",
		"unknown field":        strings.TrimSuffix(string(validJSON), "}") + `,"unexpected":true}`,
		"wrong schema":         strings.Replace(string(validJSON), model.SchemaVersion, "v99", 1),
		"multiple values":      string(validJSON) + ` {}`,
		"unsupported type":     observationJSON(t, model.Asset{Type: model.AssetService, NativeID: "service-1"}, nil),
		"unsupported volume":   observationJSON(t, model.Asset{Type: model.AssetVolume, NativeID: "disk-1"}, nil),
		"unsupported software": observationJSON(t, model.Asset{Type: model.AssetSoftware, NativeID: "software-1"}, nil),
		"unsupported vm":       observationJSON(t, model.Asset{Type: model.AssetVirtualMachine, NativeID: "vm-1"}, nil),
		"dangling relation":    observationJSON(t, model.Asset{Type: model.AssetHost, NativeID: "host-1"}, []model.Relationship{{Type: "host_has_interface", FromNativeID: "host-1", ToNativeID: "missing"}}),
		"unsupported relation": observationJSON(t, model.Asset{Type: model.AssetHost, NativeID: "host-1"}, []model.Relationship{{Type: "vm_runs_on_host", FromNativeID: "host-1", ToNativeID: "host-1"}}),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeJSONLines(strings.NewReader(input)); err == nil {
				t.Fatal("expected input to be rejected")
			}
		})
	}
}

func TestDecodeJSONLinesRejectsConflictingSourceClass(t *testing.T) {
	first := testEnvelope(model.Asset{Type: model.AssetHost, NativeID: "same"})
	second := testEnvelope(model.Asset{Type: model.AssetNetworkInterface, NativeID: "same"})
	var input bytes.Buffer
	_ = json.NewEncoder(&input).Encode(first)
	_ = json.NewEncoder(&input).Encode(second)
	if _, err := DecodeJSONLines(&input); err == nil || !strings.Contains(err.Error(), "changes ServiceNow class") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeJSONLinesRejectsBounds(t *testing.T) {
	t.Run("input bytes", func(t *testing.T) {
		if _, err := DecodeJSONLines(strings.NewReader(strings.Repeat("x", MaxInputBytes+1))); err == nil {
			t.Fatal("expected oversized input rejection")
		}
	})
	t.Run("record bytes", func(t *testing.T) {
		if _, err := DecodeJSONLines(strings.NewReader(strings.Repeat("x", MaxRecordBytes+1))); err == nil {
			t.Fatal("expected oversized record rejection")
		}
	})
	t.Run("envelopes", func(t *testing.T) {
		line := observationJSON(t, model.Asset{Type: model.AssetHost, NativeID: "same"}, nil)
		if _, err := DecodeJSONLines(strings.NewReader(strings.Repeat(line+"\n", MaxEnvelopes+1))); err == nil {
			t.Fatal("expected envelope count rejection")
		}
	})
	t.Run("depth", func(t *testing.T) {
		prefix := fmt.Sprintf(`{"schema_version":%q,"observation_id":"o","site_id":"s","collector_id":"c","plugin":"p","observed_at":%q,"assets":[{"type":"host","native_id":"h","attributes":{"nested":`, model.SchemaVersion, time.Now().UTC().Format(time.RFC3339Nano))
		input := prefix + strings.Repeat("[", MaxJSONDepth) + "0" + strings.Repeat("]", MaxJSONDepth) + "}}]}"
		if _, err := DecodeJSONLines(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "nesting") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestPreviewRejectsExcessiveUniqueItems(t *testing.T) {
	assets := make([]model.Asset, MaxItems+1)
	for index := range assets {
		assets[index] = model.Asset{Type: model.AssetHost, NativeID: fmt.Sprintf("host-%d", index)}
	}
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	if _, err := p.Preview(t.Context(), []model.ObservationEnvelope{testEnvelope(assets...)}); err == nil {
		t.Fatal("expected item count rejection")
	}
}

func TestPreviewCopiesOnlyReviewedAttributes(t *testing.T) {
	envelope := testEnvelope(
		model.Asset{Type: model.AssetHost, NativeID: "host-1", Attributes: map[string]any{"sys_id": "must-not-pass", "os": "linux"}},
		model.Asset{Type: model.AssetNetworkInterface, NativeID: "nic-1", Attributes: map[string]any{"mac_address": "00:11:22:33:44:55", "sys_id": "must-not-pass"}},
	)
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	preview, err := p.Preview(t.Context(), []model.ObservationEnvelope{envelope})
	if err != nil {
		t.Fatal(err)
	}
	payload := preview.(IREPayload)
	if _, exists := payload.Items[0].Values["sys_id"]; exists {
		t.Fatal("arbitrary host field crossed the reviewed mapping boundary")
	}
	if got := payload.Items[1].Values["mac_address"]; got != "00:11:22:33:44:55" {
		t.Fatalf("reviewed mac_address = %v", got)
	}
}

func TestPreviewMapsEveryReviewedClass(t *testing.T) {
	envelope := testEnvelope(
		model.Asset{Type: model.AssetHost, NativeID: "host-1"},
		model.Asset{Type: model.AssetNetworkInterface, NativeID: "nic-1"},
	)
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	preview, err := p.Preview(t.Context(), []model.ObservationEnvelope{envelope})
	if err != nil {
		t.Fatal(err)
	}
	payload := preview.(IREPayload)
	want := []string{"cmdb_ci_computer", "cmdb_ci_network_adapter"}
	for index, className := range want {
		if payload.Items[index].ClassName != className {
			t.Fatalf("item %d class = %q, want %q", index, payload.Items[index].ClassName, className)
		}
	}
}

func TestPreviewRejectsExcessiveUniqueRelationships(t *testing.T) {
	assets := []model.Asset{{Type: model.AssetHost, NativeID: "host-0"}}
	for index := 0; index < 50; index++ {
		assets = append(assets, model.Asset{Type: model.AssetNetworkInterface, NativeID: fmt.Sprintf("nic-%d", index)})
	}
	envelope := testEnvelope(assets...)
	for hostIndex := 0; hostIndex < 50; hostIndex++ {
		hostID := fmt.Sprintf("host-%d", hostIndex)
		if hostIndex > 0 {
			envelope.Assets = append(envelope.Assets, model.Asset{Type: model.AssetHost, NativeID: hostID})
		}
		for nicIndex := 0; nicIndex < 50; nicIndex++ {
			envelope.Relationships = append(envelope.Relationships, model.Relationship{Type: "host_has_interface", FromNativeID: hostID, ToNativeID: fmt.Sprintf("nic-%d", nicIndex)})
		}
	}
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	if _, err := p.Preview(t.Context(), []model.ObservationEnvelope{envelope}); err == nil {
		t.Fatal("expected relationship count rejection")
	}
}

func observationJSON(t *testing.T, asset model.Asset, relationships []model.Relationship) string {
	t.Helper()
	envelope := testEnvelope(asset)
	envelope.Relationships = relationships
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
