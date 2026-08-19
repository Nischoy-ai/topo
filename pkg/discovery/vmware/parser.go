package vmware

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type Interface struct {
	Name, MAC string
}

type HostInventory struct {
	NativeID                string
	Name                    string
	Vendor, Model, CPUModel string
	CPUMhz                  int32
	MemoryBytes             int64
	Interfaces              []Interface
}

type VMInventory struct {
	NativeID      string
	Name          string
	GuestFullName string
	NumCPU        int32
	MemoryMB      int32
	PowerState    string
	HostNativeID  string
	Interfaces    []Interface
}

type Inventory struct {
	Hosts []HostInventory
	VMs   []VMInventory
}

// mapHosts maps raw vSphere HostSystem objects to Topo's host inventory,
// keyed by the host's stable hardware UUID rather than its vCenter
// inventory path or managed object reference (which change when a host is
// moved between clusters or folders). It also returns a managed-object-
// reference-to-NativeID index so mapVMs can resolve each VM's running host
// without a second round trip. A host missing a hardware UUID is skipped
// rather than failing the whole target — vCenter guarantees one in
// practice, and one malformed host should not discard an otherwise healthy
// inventory.
func mapHosts(hosts []mo.HostSystem) ([]HostInventory, map[string]string) {
	inventories := make([]HostInventory, 0, len(hosts))
	byMoRef := make(map[string]string, len(hosts))
	for _, host := range hosts {
		if host.Summary.Hardware == nil || host.Summary.Hardware.Uuid == "" {
			continue
		}
		nativeID := host.Summary.Hardware.Uuid
		var pnics []types.PhysicalNic
		if host.Config != nil && host.Config.Network != nil {
			pnics = host.Config.Network.Pnic
		}
		inventories = append(inventories, HostInventory{
			NativeID:    nativeID,
			Name:        host.Name,
			Vendor:      host.Summary.Hardware.Vendor,
			Model:       host.Summary.Hardware.Model,
			CPUModel:    host.Summary.Hardware.CpuModel,
			CPUMhz:      host.Summary.Hardware.CpuMhz,
			MemoryBytes: host.Summary.Hardware.MemorySize,
			Interfaces:  hostInterfaces(pnics),
		})
		byMoRef[host.Self.Value] = nativeID
	}
	return inventories, byMoRef
}

// mapVMs maps raw vSphere VirtualMachine objects to Topo's VM inventory.
// NativeID prefers the VC-managed InstanceUuid, which stays stable across
// clones and host motion, falling back to the BIOS UUID for standalone
// ESXi hosts that have no vCenter to assign one. A VM missing both is
// skipped rather than failing the whole target, for the same reason a
// malformed host is skipped in mapHosts.
func mapVMs(vms []mo.VirtualMachine, hostsByMoRef map[string]string) []VMInventory {
	inventories := make([]VMInventory, 0, len(vms))
	for _, vm := range vms {
		nativeID := vm.Summary.Config.InstanceUuid
		if nativeID == "" {
			nativeID = vm.Summary.Config.Uuid
		}
		if nativeID == "" {
			continue
		}
		var hostNativeID string
		if vm.Summary.Runtime.Host != nil {
			hostNativeID = hostsByMoRef[vm.Summary.Runtime.Host.Value]
		}
		var devices []types.BaseVirtualDevice
		if vm.Config != nil {
			devices = vm.Config.Hardware.Device
		}
		inventories = append(inventories, VMInventory{
			NativeID:      nativeID,
			Name:          vm.Name,
			GuestFullName: vm.Summary.Config.GuestFullName,
			NumCPU:        vm.Summary.Config.NumCpu,
			MemoryMB:      vm.Summary.Config.MemorySizeMB,
			PowerState:    string(vm.Summary.Runtime.PowerState),
			HostNativeID:  hostNativeID,
			Interfaces:    vmInterfaces(devices),
		})
	}
	return inventories
}

func hostInterfaces(pnics []types.PhysicalNic) []Interface {
	interfaces := make([]Interface, 0, len(pnics))
	for _, nic := range pnics {
		if nic.Mac == "" || nic.Device == "" {
			continue
		}
		interfaces = append(interfaces, Interface{Name: nic.Device, MAC: strings.ToLower(nic.Mac)})
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	return interfaces
}

func vmInterfaces(devices []types.BaseVirtualDevice) []Interface {
	cards := object.VirtualDeviceList(devices).SelectByType((*types.VirtualEthernetCard)(nil))
	interfaces := make([]Interface, 0, len(cards))
	for _, device := range cards {
		ethernetCard, ok := device.(types.BaseVirtualEthernetCard)
		if !ok {
			continue
		}
		card := ethernetCard.GetVirtualEthernetCard()
		if card.MacAddress == "" {
			continue
		}
		interfaces = append(interfaces, Interface{Name: fmt.Sprintf("eth-%d", card.Key), MAC: strings.ToLower(card.MacAddress)})
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Name < interfaces[j].Name })
	return interfaces
}

func (inv Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "vmware-vcenter", Collected: now, Confidence: 1}}
	var assets []model.Asset
	var relationships []model.Relationship

	for _, host := range inv.Hosts {
		assets = append(assets, model.Asset{
			Type:     model.AssetHost,
			NativeID: host.NativeID,
			Name:     host.Name,
			Identifiers: map[string]string{
				"hardware_uuid": host.NativeID,
				"hostname":      host.Name,
			},
			Attributes: map[string]any{
				"vendor":       host.Vendor,
				"model":        host.Model,
				"cpu_model":    host.CPUModel,
				"cpu_mhz":      host.CPUMhz,
				"memory_bytes": host.MemoryBytes,
			},
			Evidence: ev,
		})
		for _, iface := range host.Interfaces {
			native := host.NativeID + ":interface:" + iface.Name
			assets = append(assets, model.Asset{
				Type:     model.AssetNetworkInterface,
				NativeID: native,
				Name:     iface.Name,
				Identifiers: map[string]string{
					"host_hardware_uuid": host.NativeID,
					"mac_address":        iface.MAC,
				},
				Attributes: map[string]any{"mac_address": iface.MAC},
				Evidence:   ev,
			})
			relationships = append(relationships, model.Relationship{Type: "host_has_interface", FromNativeID: host.NativeID, ToNativeID: native, Evidence: ev})
		}
	}

	for _, vm := range inv.VMs {
		assets = append(assets, model.Asset{
			Type:     model.AssetVirtualMachine,
			NativeID: vm.NativeID,
			Name:     vm.Name,
			Identifiers: map[string]string{
				"instance_uuid": vm.NativeID,
				"vm_name":       vm.Name,
			},
			Attributes: map[string]any{
				"guest_full_name": vm.GuestFullName,
				"num_cpu":         vm.NumCPU,
				"memory_mb":       vm.MemoryMB,
				"power_state":     vm.PowerState,
			},
			Evidence: ev,
		})
		for _, iface := range vm.Interfaces {
			native := vm.NativeID + ":interface:" + iface.Name
			assets = append(assets, model.Asset{
				Type:     model.AssetNetworkInterface,
				NativeID: native,
				Name:     iface.Name,
				Identifiers: map[string]string{
					"vm_instance_uuid": vm.NativeID,
					"mac_address":      iface.MAC,
				},
				Attributes: map[string]any{"mac_address": iface.MAC},
				Evidence:   ev,
			})
			relationships = append(relationships, model.Relationship{Type: "vm_has_interface", FromNativeID: vm.NativeID, ToNativeID: native, Evidence: ev})
		}
		if vm.HostNativeID != "" {
			relationships = append(relationships, model.Relationship{Type: "vm_runs_on_host", FromNativeID: vm.NativeID, ToNativeID: vm.HostNativeID, Evidence: ev})
		}
	}

	return assets, relationships
}
