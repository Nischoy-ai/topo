package vmware

import (
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func TestMapHostsSkipsHostsWithoutHardwareUUID(t *testing.T) {
	hosts := []mo.HostSystem{
		{ManagedEntity: mo.ManagedEntity{ExtensibleManagedObject: mo.ExtensibleManagedObject{Self: types.ManagedObjectReference{Type: "HostSystem", Value: "host-1"}}, Name: "esx1"}, Summary: types.HostListSummary{Hardware: &types.HostHardwareSummary{Uuid: "uuid-1"}}},
		{ManagedEntity: mo.ManagedEntity{ExtensibleManagedObject: mo.ExtensibleManagedObject{Self: types.ManagedObjectReference{Type: "HostSystem", Value: "host-2"}}, Name: "esx2"}, Summary: types.HostListSummary{Hardware: nil}},
		{ManagedEntity: mo.ManagedEntity{ExtensibleManagedObject: mo.ExtensibleManagedObject{Self: types.ManagedObjectReference{Type: "HostSystem", Value: "host-3"}}, Name: "esx3"}, Summary: types.HostListSummary{Hardware: &types.HostHardwareSummary{Uuid: ""}}},
	}
	inventories, byMoRef := mapHosts(hosts)
	if len(inventories) != 1 || inventories[0].NativeID != "uuid-1" {
		t.Fatalf("got %#v", inventories)
	}
	if len(byMoRef) != 1 || byMoRef["host-1"] != "uuid-1" {
		t.Fatalf("got moref index %#v", byMoRef)
	}
}

func TestMapHostsExtractsInterfaces(t *testing.T) {
	host := mo.HostSystem{
		ManagedEntity: mo.ManagedEntity{ExtensibleManagedObject: mo.ExtensibleManagedObject{Self: types.ManagedObjectReference{Value: "host-1"}}, Name: "esx1"},
		Summary:       types.HostListSummary{Hardware: &types.HostHardwareSummary{Uuid: "uuid-1", Vendor: "Dell", Model: "R640"}},
		Config: &types.HostConfigInfo{Network: &types.HostNetworkInfo{Pnic: []types.PhysicalNic{
			{Device: "vmnic0", Mac: "00:11:22:33:44:55"},
			{Device: "vmnic1", Mac: ""},
		}}},
	}
	inventories, _ := mapHosts([]mo.HostSystem{host})
	if len(inventories) != 1 {
		t.Fatalf("got %d hosts", len(inventories))
	}
	ifaces := inventories[0].Interfaces
	if len(ifaces) != 1 || ifaces[0].Name != "vmnic0" || ifaces[0].MAC != "00:11:22:33:44:55" {
		t.Fatalf("got interfaces %#v", ifaces)
	}
}

func TestMapVMsPrefersInstanceUUIDAndFallsBackToUUID(t *testing.T) {
	vms := []mo.VirtualMachine{
		{ManagedEntity: mo.ManagedEntity{Name: "vm1"}, Summary: types.VirtualMachineSummary{Config: types.VirtualMachineConfigSummary{InstanceUuid: "instance-1", Uuid: "bios-1"}}},
		{ManagedEntity: mo.ManagedEntity{Name: "vm2"}, Summary: types.VirtualMachineSummary{Config: types.VirtualMachineConfigSummary{Uuid: "bios-2"}}},
		{ManagedEntity: mo.ManagedEntity{Name: "vm3"}, Summary: types.VirtualMachineSummary{Config: types.VirtualMachineConfigSummary{}}},
	}
	inventories := mapVMs(vms, nil)
	if len(inventories) != 2 {
		t.Fatalf("got %d VMs, want 2 (vm3 has no identity)", len(inventories))
	}
	if inventories[0].NativeID != "instance-1" {
		t.Fatalf("got NativeID %q, want instance-1", inventories[0].NativeID)
	}
	if inventories[1].NativeID != "bios-2" {
		t.Fatalf("got NativeID %q, want bios-2 (fallback)", inventories[1].NativeID)
	}
}

func TestMapVMsResolvesRunningHost(t *testing.T) {
	byMoRef := map[string]string{"host-1": "uuid-1"}
	vms := []mo.VirtualMachine{
		{ManagedEntity: mo.ManagedEntity{Name: "vm1"}, Summary: types.VirtualMachineSummary{
			Config:  types.VirtualMachineConfigSummary{InstanceUuid: "instance-1"},
			Runtime: types.VirtualMachineRuntimeInfo{Host: &types.ManagedObjectReference{Value: "host-1"}},
		}},
	}
	inventories := mapVMs(vms, byMoRef)
	if len(inventories) != 1 || inventories[0].HostNativeID != "uuid-1" {
		t.Fatalf("got %#v", inventories)
	}
}

func TestVMInterfacesExtractsMacAddresses(t *testing.T) {
	devices := []types.BaseVirtualDevice{
		&types.VirtualE1000{VirtualEthernetCard: types.VirtualEthernetCard{VirtualDevice: types.VirtualDevice{Key: 4000}, MacAddress: "aa:bb:cc:dd:ee:ff"}},
		&types.VirtualDisk{VirtualDevice: types.VirtualDevice{Key: 2000}},
	}
	interfaces := vmInterfaces(devices)
	if len(interfaces) != 1 || interfaces[0].MAC != "aa:bb:cc:dd:ee:ff" || interfaces[0].Name != "eth-4000" {
		t.Fatalf("got %#v", interfaces)
	}
}

func TestInventoryAssetsProducesStableIdentityAndRelationships(t *testing.T) {
	inv := Inventory{
		Hosts: []HostInventory{{NativeID: "uuid-1", Name: "esx1", Interfaces: []Interface{{Name: "vmnic0", MAC: "00:11:22:33:44:55"}}}},
		VMs:   []VMInventory{{NativeID: "instance-1", Name: "vm1", HostNativeID: "uuid-1", Interfaces: []Interface{{Name: "eth-4000", MAC: "aa:bb:cc:dd:ee:ff"}}}},
	}
	assets, relationships := inv.Assets(time.Now())
	if len(assets) != 4 {
		t.Fatalf("got %d assets, want 4 (host+iface+vm+iface)", len(assets))
	}
	wantTypes := map[string]int{}
	for _, a := range assets {
		wantTypes[string(a.Type)]++
	}
	if wantTypes[string(model.AssetHost)] != 1 || wantTypes[string(model.AssetVirtualMachine)] != 1 || wantTypes[string(model.AssetNetworkInterface)] != 2 {
		t.Fatalf("got asset type counts %#v", wantTypes)
	}
	relTypes := map[string]int{}
	for _, r := range relationships {
		relTypes[r.Type]++
	}
	if relTypes["host_has_interface"] != 1 || relTypes["vm_has_interface"] != 1 || relTypes["vm_runs_on_host"] != 1 {
		t.Fatalf("got relationship type counts %#v", relTypes)
	}
}
