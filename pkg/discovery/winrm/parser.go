package winrm

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type Inventory struct {
	Hostname, MachineID, Domain, Manufacturer, Model string
	Serial, OSName, OSVersion, OSBuild, Architecture string
	DomainJoined                                     bool
	CPUCount, MemoryMB                               int
	Interfaces                                       []Interface
	Volumes                                          []Volume
	Services                                         []Service
	Patches                                          []Patch
}

type Interface struct {
	Index     int
	Name, MAC string
	Addresses []string
}

type Volume struct {
	DeviceID   string `json:"device_id"`
	Label      string `json:"label,omitempty"`
	FileSystem string `json:"file_system,omitempty"`
	SizeBytes  uint64 `json:"size_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type Service struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	State       string `json:"state,omitempty"`
	StartMode   string `json:"start_mode,omitempty"`
	StartName   string `json:"start_name,omitempty"`
}

type Patch struct {
	HotFixID    string `json:"hotfix_id"`
	Description string `json:"description,omitempty"`
	InstalledOn string `json:"installed_on,omitempty"`
}

func parseInventory(results map[string][]object) (Inventory, error) {
	computer, err := oneObject(OperationComputerSystem, results[OperationComputerSystem])
	if err != nil {
		return Inventory{}, err
	}
	product, err := oneObject(OperationComputerSystemProduct, results[OperationComputerSystemProduct])
	if err != nil {
		return Inventory{}, err
	}
	bios, err := oneObject(OperationBIOS, results[OperationBIOS])
	if err != nil {
		return Inventory{}, err
	}
	os, err := oneObject(OperationOperatingSystem, results[OperationOperatingSystem])
	if err != nil {
		return Inventory{}, err
	}

	cpuCount, err := positiveInt("NumberOfLogicalProcessors", first(computer, "NumberOfLogicalProcessors"))
	if err != nil {
		return Inventory{}, err
	}
	memoryBytes, err := positiveUint("TotalPhysicalMemory", first(computer, "TotalPhysicalMemory"))
	if err != nil {
		return Inventory{}, err
	}
	hostname := first(computer, "Name")
	machineID := strings.ToLower(first(product, "UUID"))
	serial := first(bios, "SerialNumber")
	if hostname == "" || machineID == "" || serial == "" {
		return Inventory{}, fmt.Errorf("empty hostname, machine UUID, or BIOS serial")
	}

	domainJoined, err := strconv.ParseBool(strings.TrimSpace(first(computer, "PartOfDomain")))
	if err != nil {
		return Inventory{}, fmt.Errorf("invalid PartOfDomain %q", first(computer, "PartOfDomain"))
	}
	return Inventory{
		Hostname:     hostname,
		MachineID:    machineID,
		Domain:       first(computer, "Domain"),
		DomainJoined: domainJoined,
		Manufacturer: first(computer, "Manufacturer"),
		Model:        first(computer, "Model"),
		Serial:       serial,
		OSName:       first(os, "Caption"),
		OSVersion:    first(os, "Version"),
		OSBuild:      first(os, "BuildNumber"),
		Architecture: normalizeArchitecture(first(os, "OSArchitecture")),
		CPUCount:     cpuCount,
		MemoryMB:     int(memoryBytes / (1024 * 1024)),
	}, nil
}

func parseInterfaces(objects []object) ([]Interface, error) {
	interfaces := make([]Interface, 0, len(objects))
	for _, value := range objects {
		index, err := positiveInt("InterfaceIndex", first(value, "InterfaceIndex"))
		if err != nil {
			return nil, err
		}
		mac := strings.ToLower(first(value, "MACAddress"))
		if mac == "" {
			return nil, errorsForField("MACAddress")
		}
		addresses := append([]string(nil), value["IPAddress"]...)
		subnets := value["IPSubnet"]
		for index := range addresses {
			if index < len(subnets) {
				addresses[index] = addressWithPrefix(addresses[index], subnets[index])
			}
		}
		interfaces = append(interfaces, Interface{Index: index, Name: first(value, "Description"), MAC: mac, Addresses: addresses})
	}
	return interfaces, nil
}

func parseVolumes(objects []object) ([]Volume, error) {
	volumes := make([]Volume, 0, len(objects))
	for _, value := range objects {
		deviceID := first(value, "DeviceID")
		if deviceID == "" {
			return nil, errorsForField("DeviceID")
		}
		size, err := positiveUint("Size", first(value, "Size"))
		if err != nil {
			return nil, err
		}
		free, err := nonNegativeUint("FreeSpace", first(value, "FreeSpace"))
		if err != nil {
			return nil, err
		}
		if free > size {
			return nil, fmt.Errorf("FreeSpace %d exceeds Size %d for volume %q", free, size, deviceID)
		}
		volumes = append(volumes, Volume{DeviceID: deviceID, Label: first(value, "VolumeName"), FileSystem: first(value, "FileSystem"), SizeBytes: size, FreeBytes: free})
	}
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].DeviceID < volumes[j].DeviceID })
	return volumes, nil
}

func parseServices(objects []object) ([]Service, error) {
	services := make([]Service, 0, len(objects))
	for _, value := range objects {
		name := first(value, "Name")
		if name == "" {
			return nil, errorsForField("Name")
		}
		services = append(services, Service{Name: name, DisplayName: first(value, "DisplayName"), State: first(value, "State"), StartMode: first(value, "StartMode"), StartName: first(value, "StartName")})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}

func parsePatches(objects []object) ([]Patch, error) {
	patches := make([]Patch, 0, len(objects))
	for _, value := range objects {
		hotFixID := first(value, "HotFixID")
		if hotFixID == "" {
			return nil, errorsForField("HotFixID")
		}
		patches = append(patches, Patch{HotFixID: hotFixID, Description: first(value, "Description"), InstalledOn: first(value, "InstalledOn")})
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].HotFixID < patches[j].HotFixID })
	return patches, nil
}

func (inventory Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	evidence := []model.Evidence{{Source: "winrm-windows", Collected: now, Confidence: 1}}
	attributes := map[string]any{
		"os":                "windows",
		"os_name":           inventory.OSName,
		"os_version":        inventory.OSVersion,
		"os_build":          inventory.OSBuild,
		"architecture":      inventory.Architecture,
		"cpu_logical_count": inventory.CPUCount,
		"memory_mb":         inventory.MemoryMB,
		"domain":            inventory.Domain,
		"domain_joined":     inventory.DomainJoined,
		"manufacturer":      inventory.Manufacturer,
		"model":             inventory.Model,
	}
	if inventory.Volumes != nil {
		attributes["volumes"] = inventory.Volumes
	}
	if inventory.Services != nil {
		attributes["services"] = inventory.Services
	}
	if inventory.Patches != nil {
		attributes["patches"] = inventory.Patches
	}
	host := model.Asset{
		Type:     model.AssetHost,
		NativeID: inventory.MachineID,
		Name:     inventory.Hostname,
		Identifiers: map[string]string{
			"machine_id":    inventory.MachineID,
			"serial_number": inventory.Serial,
			"hostname":      inventory.Hostname,
		},
		Attributes: attributes,
		Evidence:   evidence,
	}
	assets := []model.Asset{host}
	var relationships []model.Relationship
	for _, networkInterface := range inventory.Interfaces {
		nativeID := fmt.Sprintf("%s:interface:%d", inventory.MachineID, networkInterface.Index)
		assets = append(assets, model.Asset{
			Type:     model.AssetNetworkInterface,
			NativeID: nativeID,
			Name:     networkInterface.Name,
			Identifiers: map[string]string{
				"host_machine_id": inventory.MachineID,
				"mac_address":     networkInterface.MAC,
			},
			Attributes: map[string]any{"mac_address": networkInterface.MAC, "addresses": networkInterface.Addresses},
			Evidence:   evidence,
		})
		relationships = append(relationships, model.Relationship{Type: "host_has_interface", FromNativeID: inventory.MachineID, ToNativeID: nativeID, Evidence: evidence})
	}
	return assets, relationships
}

func oneObject(name string, objects []object) (object, error) {
	if len(objects) != 1 {
		return nil, fmt.Errorf("%s returned %d objects, want 1", name, len(objects))
	}
	return objects[0], nil
}

func first(value object, name string) string {
	if len(value[name]) == 0 {
		return ""
	}
	return strings.TrimSpace(value[name][0])
}

func positiveInt(name, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return 0, fmt.Errorf("invalid %s %q", name, strings.TrimSpace(raw))
	}
	return value, nil
}

func positiveUint(name, raw string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("invalid %s %q", name, strings.TrimSpace(raw))
	}
	return value, nil
}

func nonNegativeUint(name, raw string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, strings.TrimSpace(raw))
	}
	return value, nil
}

func normalizeArchitecture(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "64-bit", "amd64", "x64", "x86_64":
		return "x86_64"
	case "32-bit", "x86", "i386":
		return "x86"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return strings.TrimSpace(value)
	}
}

func addressWithPrefix(address, subnet string) string {
	address = strings.TrimSpace(address)
	subnet = strings.TrimSpace(subnet)
	if address == "" || subnet == "" || strings.Contains(address, "/") {
		return address
	}
	if prefix, err := strconv.Atoi(subnet); err == nil {
		return fmt.Sprintf("%s/%d", address, prefix)
	}
	ip := net.ParseIP(subnet)
	if ip == nil {
		return address
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		ones, _ := net.IPMask(ipv4).Size()
		return fmt.Sprintf("%s/%d", address, ones)
	}
	return address
}

func errorsForField(name string) error { return fmt.Errorf("empty %s", name) }
