// Package winrm discovers Windows hosts through audited WS-Management operations.
package winrm

const (
	ActionEnumerate = "http://schemas.xmlsoap.org/ws/2004/09/enumeration/Enumerate"
	ActionPull      = "http://schemas.xmlsoap.org/ws/2004/09/enumeration/Pull"
	DialectWQL      = "http://schemas.microsoft.com/wbem/wsman/1/WQL"

	OperationComputerSystem        = "computer_system"
	OperationComputerSystemProduct = "computer_system_product"
	OperationBIOS                  = "bios"
	OperationOperatingSystem       = "operating_system"
	OperationNetwork               = "network"
	OperationVolumes               = "volumes"
	OperationServices              = "services"
	OperationPatches               = "patches"
)

// Operation is one compiled-in WS-Management operation. Jobs can select
// targets, but cannot add resource URIs, WQL, PowerShell, or other remote text.
type Operation struct {
	Name        string
	ResourceURI string
	Query       string
	Required    bool
}

var auditedOperations = []Operation{
	{Name: OperationComputerSystem, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystem", Required: true},
	{Name: OperationComputerSystemProduct, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_ComputerSystemProduct", Required: true},
	{Name: OperationBIOS, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_BIOS", Required: true},
	{Name: OperationOperatingSystem, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem", Required: true},
	{Name: OperationNetwork, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_NetworkAdapterConfiguration", Query: "SELECT Description, InterfaceIndex, MACAddress, IPAddress, IPSubnet FROM Win32_NetworkAdapterConfiguration WHERE IPEnabled = TRUE", Required: false},
	{Name: OperationVolumes, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_LogicalDisk", Query: "SELECT DeviceID, VolumeName, FileSystem, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType = 3", Required: false},
	{Name: OperationServices, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_Service", Query: "SELECT Name, DisplayName, State, StartMode, StartName FROM Win32_Service", Required: false},
	{Name: OperationPatches, ResourceURI: "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_QuickFixEngineering", Query: "SELECT HotFixID, Description, InstalledOn FROM Win32_QuickFixEngineering", Required: false},
}

// AuditedOperations returns a copy so callers cannot mutate the contract.
func AuditedOperations() []Operation {
	return append([]Operation(nil), auditedOperations...)
}

// MatchOperation accepts only an exact compiled-in action, resource URI, and
// query tuple. It is shared with Topo Lab so the simulator enforces the same
// contract as the production client.
func MatchOperation(action, resourceURI, query string) (Operation, bool) {
	if action != ActionEnumerate {
		return Operation{}, false
	}
	for _, operation := range auditedOperations {
		if operation.ResourceURI == resourceURI && operation.Query == query {
			return operation, true
		}
	}
	return Operation{}, false
}
