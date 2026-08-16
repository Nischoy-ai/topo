package winrm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func TestOnlyExactAuditedOperationsAreAllowed(t *testing.T) {
	operations := AuditedOperations()
	if len(operations) != 8 {
		t.Fatalf("got %d audited operations, want 8", len(operations))
	}
	for _, operation := range operations {
		matched, ok := MatchOperation(ActionEnumerate, operation.ResourceURI, operation.Query)
		if !ok || matched.Name != operation.Name {
			t.Fatalf("audited operation %q was rejected", operation.Name)
		}
	}
	if _, ok := MatchOperation(ActionEnumerate, operations[0].ResourceURI+";whoami", ""); ok {
		t.Fatal("arbitrary resource URI was accepted")
	}
	if _, ok := MatchOperation(ActionEnumerate, operations[4].ResourceURI, operations[4].Query+"; DELETE"); ok {
		t.Fatal("altered WQL was accepted")
	}
	if _, ok := MatchOperation("http://example.test/PowerShell", operations[0].ResourceURI, ""); ok {
		t.Fatal("arbitrary SOAP action was accepted")
	}
	operations[0].ResourceURI = "mutated"
	if AuditedOperations()[0].ResourceURI == "mutated" {
		t.Fatal("caller mutated the audited operation contract")
	}
}

func TestSOAPEnvelopeContainsOnlyAuditedRouting(t *testing.T) {
	operation := AuditedOperations()[4]
	envelope := enumerateEnvelope("https://windows.example/wsman", operation, time.Second)
	if !strings.Contains(string(envelope), `<w:MaxElements>128</w:MaxElements>`) || !strings.Contains(string(envelope), `<w:MaxEnvelopeSize s:mustUnderstand="true">4194304</w:MaxEnvelopeSize>`) {
		t.Fatalf("enumeration envelope omitted Windows WS-Management bounds: %s", envelope)
	}
	request, err := ParseSOAPRequest(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != ActionEnumerate || request.ResourceURI != operation.ResourceURI || request.Query != operation.Query || request.FilterDialect != DialectWQL || request.BodyOperation != "Enumerate" {
		t.Fatalf("unexpected SOAP routing: %#v", request)
	}

	arbitrary := strings.Replace(string(enumerateEnvelope("https://windows.example/wsman", operation, time.Second)), "<n:Enumerate>", "<n:Command>", 1)
	arbitrary = strings.Replace(arbitrary, "</n:Enumerate>", "</n:Command>", 1)
	request, err = ParseSOAPRequest([]byte(arbitrary))
	if err != nil {
		t.Fatal(err)
	}
	if request.BodyOperation != "Command" {
		t.Fatalf("arbitrary body operation was not exposed for rejection: %#v", request)
	}
}

func TestParseEnumerationPageAndInventory(t *testing.T) {
	response := `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:n="http://schemas.xmlsoap.org/ws/2004/09/enumeration" xmlns:p="urn:test"><s:Body><n:PullResponse><n:Items><p:Win32_NetworkAdapterConfiguration><p:Description>primary</p:Description><p:InterfaceIndex>1</p:InterfaceIndex><p:MACAddress>02:54:00:00:00:01</p:MACAddress><p:IPAddress><p:string>10.1.0.2</p:string><p:string>fe80::1</p:string></p:IPAddress><p:IPSubnet><p:string>255.255.255.0</p:string><p:string>64</p:string></p:IPSubnet></p:Win32_NetworkAdapterConfiguration></n:Items><n:EndOfSequence/></n:PullResponse></s:Body></s:Envelope>`
	page, err := parseEnumerationPage([]byte(response))
	if err != nil {
		t.Fatal(err)
	}
	if !page.Done || len(page.Items) != 1 {
		t.Fatalf("unexpected page: %#v", page)
	}
	interfaces, err := parseInterfaces(page.Items)
	if err != nil {
		t.Fatal(err)
	}
	want := []Interface{{Index: 1, Name: "primary", MAC: "02:54:00:00:00:01", Addresses: []string{"10.1.0.2/24", "fe80::1/64"}}}
	if !reflect.DeepEqual(interfaces, want) {
		t.Fatalf("got %#v, want %#v", interfaces, want)
	}
}

func TestParseRequiredWindowsInventory(t *testing.T) {
	results := map[string][]object{
		OperationComputerSystem: {{
			"Name":                      {"WIN-01"},
			"Domain":                    {"example.test"},
			"PartOfDomain":              {"true"},
			"Manufacturer":              {"Example"},
			"Model":                     {"Server"},
			"NumberOfLogicalProcessors": {"8"},
			"TotalPhysicalMemory":       {"17179869184"},
		}},
		OperationComputerSystemProduct: {{"UUID": {"AABBCCDD-0000-1111-2222-334455667788"}}},
		OperationBIOS:                  {{"SerialNumber": {"SERIAL-1"}}},
		OperationOperatingSystem:       {{"Caption": {"Microsoft Windows Server 2022 Standard"}, "Version": {"10.0.20348"}, "BuildNumber": {"20348"}, "OSArchitecture": {"64-bit"}}},
	}
	inventory, err := parseInventory(results)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.MachineID != "aabbccdd-0000-1111-2222-334455667788" || inventory.MemoryMB != 16384 || inventory.Architecture != "x86_64" || !inventory.DomainJoined {
		t.Fatalf("unexpected inventory: %#v", inventory)
	}
}

func TestParseOptionalWindowsInventory(t *testing.T) {
	volumes, err := parseVolumes([]object{
		{"DeviceID": {"D:"}, "VolumeName": {"Data"}, "FileSystem": {"NTFS"}, "Size": {"200"}, "FreeSpace": {"0"}},
		{"DeviceID": {"C:"}, "VolumeName": {"System"}, "FileSystem": {"NTFS"}, "Size": {"100"}, "FreeSpace": {"40"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantVolumes := []Volume{
		{DeviceID: "C:", Label: "System", FileSystem: "NTFS", SizeBytes: 100, FreeBytes: 40},
		{DeviceID: "D:", Label: "Data", FileSystem: "NTFS", SizeBytes: 200, FreeBytes: 0},
	}
	if !reflect.DeepEqual(volumes, wantVolumes) {
		t.Fatalf("got volumes %#v, want %#v", volumes, wantVolumes)
	}

	services, err := parseServices([]object{
		{"Name": {"W32Time"}, "DisplayName": {"Windows Time"}, "State": {"Running"}, "StartMode": {"Auto"}, "StartName": {"LocalSystem"}},
		{"Name": {"WinRM"}, "DisplayName": {"Windows Remote Management"}, "State": {"Running"}, "StartMode": {"Auto"}, "StartName": {"NetworkService"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 || services[0].Name != "W32Time" || services[1].Name != "WinRM" {
		t.Fatalf("unexpected sorted services: %#v", services)
	}

	patches, err := parsePatches([]object{
		{"HotFixID": {"KB9000002"}, "Description": {"Update"}, "InstalledOn": {"8/2/2026"}},
		{"HotFixID": {"KB9000001"}, "Description": {"Security Update"}, "InstalledOn": {"8/1/2026"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 || patches[0].HotFixID != "KB9000001" || patches[1].HotFixID != "KB9000002" {
		t.Fatalf("unexpected sorted patches: %#v", patches)
	}
}

func TestOptionalInventoryRejectsMalformedValues(t *testing.T) {
	if _, err := parseVolumes([]object{{"DeviceID": {"C:"}, "Size": {"100"}, "FreeSpace": {"101"}}}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("volume accepted impossible free space: %v", err)
	}
	if _, err := parseServices([]object{{"DisplayName": {"missing name"}}}); err == nil {
		t.Fatal("service accepted an empty stable name")
	}
	if _, err := parsePatches([]object{{"Description": {"missing ID"}}}); err == nil {
		t.Fatal("patch accepted an empty hotfix ID")
	}
}

func TestConfigurationEnforcesTLSLabIsolationAndSecretReferences(t *testing.T) {
	production := Plugin{}
	if err := production.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"http://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("production HTTP target was not rejected: %v", err)
	}
	labPlugin := Plugin{Config: Config{LabMode: true, Username: "lab", Password: "secret"}}
	if err := labPlugin.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"http://windows.example/wsman/host"}}); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback Lab target was not rejected: %v", err)
	}
	if err := labPlugin.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"http://127.0.0.1/wsman/host"}, Options: map[string]string{"password": "secret"}}); err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatalf("request secret was not rejected: %v", err)
	}
	if err := (Plugin{Config: Config{Username: "user", Password: "secret"}}).ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "explicit authentication mode") {
		t.Fatalf("implicit production credentials were not rejected: %v", err)
	}
	ntlmPlugin := Plugin{Config: Config{AuthMode: AuthModeNTLM, Username: `EXAMPLE\topo-reader`, Password: "secret"}}
	if err := ntlmPlugin.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err != nil {
		t.Fatalf("valid NTLM configuration was rejected: %v", err)
	}
	ntlmPlugin.Config.Password = ""
	if err := ntlmPlugin.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Fatalf("incomplete NTLM credentials were accepted: %v", err)
	}
	if err := (Plugin{Config: Config{AuthMode: "kerberos", Username: "user", Password: "secret"}}).ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported authentication mode was accepted: %v", err)
	}
	if err := (Plugin{Config: Config{LabMode: true, AuthMode: AuthModeNTLM, Username: "user", Password: "secret"}}).ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"http://127.0.0.1/wsman/host"}}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("Lab Basic and NTLM were combined: %v", err)
	}
	if err := (Plugin{Config: Config{AuthMode: AuthModeNTLM, Username: "bad\nuser", Password: "secret"}}).ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Fatalf("control character in username was accepted: %v", err)
	}
	if err := (Plugin{Config: Config{AuthMode: AuthModeNTLM, Username: "user", Password: strings.Repeat("x", 4097)}}).ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"https://windows.example/wsman"}}); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("oversized password was accepted: %v", err)
	}
}

func TestDefaultClientVerifiesTLSServerIdentity(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	plugin := Plugin{Config: Config{OperationTimeout: time.Second}}
	err := plugin.CheckConnectivity(context.Background(), discovery.Request{Targets: []string{server.URL + "/wsman"}})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("default client did not reject an untrusted TLS identity: %v", err)
	}
}
