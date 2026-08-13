package servicenow

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestPreviewProducesIREIdentityAndRelationship(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "https://example.service-now.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	e := model.ObservationEnvelope{ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Assets: []model.Asset{{Type: model.AssetHost, NativeID: "h1", Name: "host"}, {Type: model.AssetNetworkInterface, NativeID: "n1", Name: "eth0"}}, Relationships: []model.Relationship{{Type: "host_has_interface", FromNativeID: "h1", ToNativeID: "n1"}}}
	v, err := p.Preview(context.Background(), []model.ObservationEnvelope{e})
	if err != nil {
		t.Fatal(err)
	}
	payload := v.(IREPayload)
	if len(payload.Items) != 2 || len(payload.Relations) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload.Items[0].SourceInfo.SourceNativeKey != "h1" {
		t.Fatal("source native key missing")
	}
}

func TestValidateRequiresHTTPS(t *testing.T) {
	p := Publisher{Config: Config{InstanceURL: "http://example.com", DiscoverySource: "Nischoy Topo", DryRun: true}}
	if err := p.Validate(context.Background()); err == nil {
		t.Fatal("expected HTTPS validation error")
	}
}
