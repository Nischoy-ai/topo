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
