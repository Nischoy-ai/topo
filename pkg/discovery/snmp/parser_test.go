package snmp

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestParseSystemGroup(t *testing.T) {
	variables := []gosnmp.SnmpPDU{
		{Name: oidSysDescr, Type: gosnmp.OctetString, Value: []byte("Topo Lab Router")},
		{Name: "." + oidSysObjectID, Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.99999.1"},
		{Name: oidSysUpTime, Type: gosnmp.TimeTicks, Value: uint32(12345)},
		{Name: oidSysName, Type: gosnmp.OctetString, Value: []byte("lab-router-1")},
	}
	sysDescr, sysObjectID, sysName, sysUpTime, err := parseSystemGroup(variables)
	if err != nil {
		t.Fatal(err)
	}
	if sysDescr != "Topo Lab Router" || sysObjectID != "1.3.6.1.4.1.99999.1" || sysName != "lab-router-1" || sysUpTime != 12345 {
		t.Fatalf("got descr=%q objectID=%q name=%q upTime=%d", sysDescr, sysObjectID, sysName, sysUpTime)
	}
}

func TestParseSystemGroupRejectsWrongType(t *testing.T) {
	variables := []gosnmp.SnmpPDU{{Name: oidSysDescr, Type: gosnmp.Integer, Value: 0}}
	if _, _, _, _, err := parseSystemGroup(variables); err == nil {
		t.Fatal("expected error for non-OctetString sysDescr")
	}
}

func TestParseSystemGroupRejectsNoSuchObject(t *testing.T) {
	variables := []gosnmp.SnmpPDU{{Name: oidSysName, Type: gosnmp.NoSuchObject}}
	if _, _, _, _, err := parseSystemGroup(variables); err == nil {
		t.Fatal("expected error for NoSuchObject sysName")
	}
}

func TestLastOIDIndex(t *testing.T) {
	index, err := lastOIDIndex(".1.3.6.1.2.1.2.2.1.2.7")
	if err != nil {
		t.Fatal(err)
	}
	if index != 7 {
		t.Fatalf("got index %d, want 7", index)
	}
}

func TestLastOIDIndexRejectsNonNumeric(t *testing.T) {
	if _, err := lastOIDIndex("1.3.6.1.2.1.2.2.1.2.x"); err == nil {
		t.Fatal("expected error for non-numeric index")
	}
}

func TestFormatMAC(t *testing.T) {
	got := formatMAC([]byte{0x02, 0x54, 0xaa, 0xbb, 0xcc, 0xdd})
	if got != "02:54:aa:bb:cc:dd" {
		t.Fatalf("got %q", got)
	}
}

func TestInventoryAssetsProducesStableIdentityAndHostHasInterface(t *testing.T) {
	inventory := Inventory{
		EngineID: "aabbccdd", SysName: "lab-router-1", SysDescr: "desc", SysObjectID: "1.3.6.1.4.1.1.1",
		SysUpTimeTicks: 42,
		Interfaces:     []Interface{{Index: 1, Name: "primary", MAC: "02:54:aa:bb:cc:dd"}},
	}
	assets, relationships := inventory.Assets(time.Now())
	if len(assets) != 2 {
		t.Fatalf("got %d assets, want 2", len(assets))
	}
	if assets[0].NativeID != "aabbccdd" {
		t.Fatalf("host NativeID = %q, want engine ID", assets[0].NativeID)
	}
	if assets[1].NativeID != "aabbccdd:interface:1" {
		t.Fatalf("interface NativeID = %q", assets[1].NativeID)
	}
	if len(relationships) != 1 || relationships[0].Type != "host_has_interface" || relationships[0].FromNativeID != "aabbccdd" || relationships[0].ToNativeID != "aabbccdd:interface:1" {
		t.Fatalf("unexpected relationship: %#v", relationships)
	}
}
