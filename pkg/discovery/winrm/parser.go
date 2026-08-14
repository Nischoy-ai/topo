package winrm

import (
	"fmt"
	"net"
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
}

type Interface struct {
	Index     int
	Name, MAC string
	Addresses []string
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
