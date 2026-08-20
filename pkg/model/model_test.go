package model

import "testing"

func TestStableAssetIDIsOrderIndependent(t *testing.T) {
	a := Asset{Type: AssetHost, NativeID: "machine-1", Identifiers: map[string]string{"serial": "x", "hostname": "web"}}
	b := Asset{Type: AssetHost, NativeID: "machine-1", Identifiers: map[string]string{"hostname": "web", "serial": "x"}}
	if StableAssetID(a) != StableAssetID(b) {
		t.Fatal("stable ID changed with map ordering")
	}
}

func TestStableAssetIDDoesNotUseAttributes(t *testing.T) {
	a := Asset{Type: AssetHost, NativeID: "machine-1", Attributes: map[string]any{"ip": "10.0.0.1"}}
	b := Asset{Type: AssetHost, NativeID: "machine-1", Attributes: map[string]any{"ip": "10.0.0.2"}}
	if StableAssetID(a) != StableAssetID(b) {
		t.Fatal("mutable attributes must not affect identity")
	}
}

func TestStableRelationshipIDIsStableAcrossRepeatedObservations(t *testing.T) {
	a := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1"}
	b := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1"}
	if StableRelationshipID(a) != StableRelationshipID(b) {
		t.Fatal("identical relationships produced different stable IDs")
	}
}

func TestStableRelationshipIDDistinguishesEndpointsAndType(t *testing.T) {
	base := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1"}
	differentType := Relationship{Type: "vm_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1"}
	differentFrom := Relationship{Type: "host_has_interface", FromNativeID: "machine-2", ToNativeID: "machine-1:interface:1"}
	differentTo := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:2"}
	ids := map[string]bool{}
	for _, r := range []Relationship{base, differentType, differentFrom, differentTo} {
		ids[StableRelationshipID(r)] = true
	}
	if len(ids) != 4 {
		t.Fatalf("got %d distinct IDs, want 4", len(ids))
	}
}

func TestStableRelationshipIDDoesNotUseEvidence(t *testing.T) {
	a := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1", Evidence: []Evidence{{Source: "ssh-linux", Confidence: 1}}}
	b := Relationship{Type: "host_has_interface", FromNativeID: "machine-1", ToNativeID: "machine-1:interface:1", Evidence: []Evidence{{Source: "winrm-windows", Confidence: 0.5}}}
	if StableRelationshipID(a) != StableRelationshipID(b) {
		t.Fatal("mutable evidence must not affect identity")
	}
}
