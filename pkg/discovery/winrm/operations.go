// Package winrm discovers Windows hosts through audited WS-Management operations.
package winrm

import (
	"encoding/base64"
	"encoding/binary"
	"slices"
	"unicode/utf16"
)

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
	OperationSoftware              = "software"
)

const softwareInventoryScript = `$ErrorActionPreference = 'Stop'
$utf8 = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$roots = @(
    [pscustomobject]@{ Path = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall'; Architecture = '64-bit' },
    [pscustomobject]@{ Path = 'Registry::HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall'; Architecture = '32-bit' }
)
$items = foreach ($root in $roots) {
    if (-not (Test-Path -LiteralPath $root.Path)) { continue }
    foreach ($key in Get-ChildItem -LiteralPath $root.Path -ErrorAction Stop) {
        $value = Get-ItemProperty -LiteralPath $key.PSPath -ErrorAction Stop
        if (-not [string]::IsNullOrWhiteSpace([string]$value.DisplayName)) {
            [pscustomobject][ordered]@{
                name = [string]$value.DisplayName
                version = [string]$value.DisplayVersion
                publisher = [string]$value.Publisher
                install_date = [string]$value.InstallDate
                architecture = [string]$root.Architecture
                registry_key = [string]$key.PSChildName
            }
        }
    }
}
$items | Sort-Object name, version, architecture, registry_key | ForEach-Object { $_ | ConvertTo-Json -Compress }
`

// Operation is one compiled-in WS-Management operation. Jobs can select
// targets, but cannot add resource URIs, WQL, PowerShell, or other remote text.
type Operation struct {
	Name        string
	ResourceURI string
	Query       string
	Required    bool
}

// Command is one compiled-in remote executable and argument vector. It is
// deliberately separate from discovery.Request so a controller cannot supply
// PowerShell or other remote command text.
type Command struct {
	Name       string
	Executable string
	Arguments  []string
	Required   bool
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

// AuditedSoftwareCommand returns the sole remote command admitted by the
// Windows inventory plugin: a fixed UTF-16LE PowerShell script encoded for
// powershell.exe -EncodedCommand. The script reads only the two machine-wide
// uninstall registry views and emits bounded JSON records.
func AuditedSoftwareCommand() Command {
	return Command{
		Name:       OperationSoftware,
		Executable: "powershell.exe",
		Arguments:  []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(softwareInventoryScript)},
		Required:   false,
	}
}

// MatchSoftwareCommand accepts only an exact copy of the reviewed executable
// and argument vector.
func MatchSoftwareCommand(executable string, arguments []string) bool {
	audited := AuditedSoftwareCommand()
	return executable == audited.Executable && slices.Equal(arguments, audited.Arguments)
}

func encodePowerShell(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	data := make([]byte, len(codeUnits)*2)
	for index, codeUnit := range codeUnits {
		binary.LittleEndian.PutUint16(data[index*2:], codeUnit)
	}
	return base64.StdEncoding.EncodeToString(data)
}
