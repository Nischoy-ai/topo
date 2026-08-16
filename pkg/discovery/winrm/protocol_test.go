package winrm

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

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

func TestAuditedSoftwareCommandIsFixedAndReadOnly(t *testing.T) {
	command := AuditedSoftwareCommand()
	if command.Name != OperationSoftware || command.Executable != "powershell.exe" || command.Required || len(command.Arguments) != 5 {
		t.Fatalf("unexpected audited software command: %#v", command)
	}
	encoded, err := base64.StdEncoding.DecodeString(command.Arguments[4])
	if err != nil || len(encoded)%2 != 0 {
		t.Fatalf("invalid encoded PowerShell: bytes=%d err=%v", len(encoded), err)
	}
	codeUnits := make([]uint16, len(encoded)/2)
	for index := range codeUnits {
		codeUnits[index] = binary.LittleEndian.Uint16(encoded[index*2:])
	}
	script := string(utf16.Decode(codeUnits))
	if script != softwareInventoryScript {
		t.Fatal("encoded PowerShell does not match the reviewed source")
	}
	if strings.Contains(strings.ToLower(script), "win32_product") || strings.Contains(strings.ToLower(script), "uninstallstring") {
		t.Fatal("software command uses a prohibited inventory source or collects uninstall command text")
	}
	if !strings.Contains(script, `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`) || !strings.Contains(script, `HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`) {
		t.Fatal("software command omitted a machine-wide uninstall registry view")
	}
	if !MatchSoftwareCommand(command.Executable, command.Arguments) {
		t.Fatal("audited software command rejected")
	}
	command.Arguments[4] += "tampered"
	if MatchSoftwareCommand(command.Executable, command.Arguments) {
		t.Fatal("altered PowerShell was accepted")
	}
	if AuditedSoftwareCommand().Arguments[4] == command.Arguments[4] {
		t.Fatal("caller mutated the audited command contract")
	}
}

func TestShellEnvelopesExposeExactAuditedRouting(t *testing.T) {
	endpoint := "https://windows.example/wsman"
	timeout := time.Second
	create, err := ParseSOAPRequest(createShellEnvelope(endpoint, timeout))
	if err != nil {
		t.Fatal(err)
	}
	if create.Action != ActionCreate || create.ResourceURI != ShellResourceURI || create.BodyOperation != "Shell" || create.InputStreams != "stdin" || create.OutputStreams != "stdout stderr" || create.Options["WINRS_CODEPAGE"] != "65001" {
		t.Fatalf("unexpected create routing: %#v", create)
	}
	duplicateOption := strings.Replace(string(createShellEnvelope(endpoint, timeout)), `</w:OptionSet>`, `<w:Option Name="WINRS_CODEPAGE">65001</w:Option></w:OptionSet>`, 1)
	if _, err := ParseSOAPRequest([]byte(duplicateOption)); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate shell option was accepted: %v", err)
	}
	command, err := ParseSOAPRequest(commandEnvelope(endpoint, "uuid:shell-1", AuditedSoftwareCommand(), timeout))
	if err != nil {
		t.Fatal(err)
	}
	if command.Action != ActionCommand || command.ShellID != "uuid:shell-1" || !MatchSoftwareCommand(command.Command, command.Arguments) || command.Options["WINRS_SKIP_CMD_SHELL"] != "TRUE" {
		t.Fatalf("unexpected command routing: %#v", command)
	}
	receive, err := ParseSOAPRequest(receiveEnvelope(endpoint, "uuid:shell-1", "uuid:command-1", timeout))
	if err != nil {
		t.Fatal(err)
	}
	if receive.Action != ActionReceive || receive.ShellID != "uuid:shell-1" || receive.CommandID != "uuid:command-1" || receive.DesiredStream != "stdout stderr" {
		t.Fatalf("unexpected receive routing: %#v", receive)
	}
	deleteRequest, err := ParseSOAPRequest(deleteShellEnvelope(endpoint, "uuid:shell-1", timeout))
	if err != nil {
		t.Fatal(err)
	}
	if deleteRequest.Action != ActionDelete || deleteRequest.ShellID != "uuid:shell-1" || deleteRequest.BodyOperation != "" {
		t.Fatalf("unexpected delete routing: %#v", deleteRequest)
	}
}

func TestParseShellResponses(t *testing.T) {
	create := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:t="http://schemas.xmlsoap.org/ws/2004/09/transfer" xmlns:w="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"><s:Body><t:ResourceCreated><w:SelectorSet><w:Selector Name="ShellId">uuid:shell-1</w:Selector></w:SelectorSet></t:ResourceCreated></s:Body></s:Envelope>`
	shellID, err := parseCreateShellResponse([]byte(create))
	if err != nil || shellID != "uuid:shell-1" {
		t.Fatalf("shell ID=%q err=%v", shellID, err)
	}
	command := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:r="http://schemas.microsoft.com/wbem/wsman/1/windows/shell"><s:Body><r:CommandResponse><r:CommandId>uuid:command-1</r:CommandId></r:CommandResponse></s:Body></s:Envelope>`
	commandID, err := parseCommandResponse([]byte(command))
	if err != nil || commandID != "uuid:command-1" {
		t.Fatalf("command ID=%q err=%v", commandID, err)
	}
	stdout := base64.StdEncoding.EncodeToString([]byte("output"))
	receive := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:r="http://schemas.microsoft.com/wbem/wsman/1/windows/shell"><s:Body><r:ReceiveResponse><r:Stream Name="stdout" CommandId="uuid:command-1">` + stdout + `</r:Stream><r:CommandState CommandId="uuid:command-1" State="` + commandStateDone + `"><r:ExitCode>0</r:ExitCode></r:CommandState></r:ReceiveResponse></s:Body></s:Envelope>`
	result, err := parseReceiveResponse([]byte(receive), commandID)
	if err != nil || !result.Done || result.ExitCode != 0 || string(result.Stdout) != "output" {
		t.Fatalf("unexpected receive result %#v err=%v", result, err)
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

	software, err := parseSoftware([]byte("{\"name\":\"Tool B\",\"version\":\"2\",\"publisher\":\"Example\",\"install_date\":\"20260802\",\"architecture\":\"32-bit\",\"registry_key\":\"tool-b\"}\n{\"name\":\"Tool A\",\"version\":\"1\",\"publisher\":\"Example\",\"install_date\":\"20260801\",\"architecture\":\"64-bit\",\"registry_key\":\"tool-a\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(software) != 2 || software[0].Name != "Tool A" || software[0].Architecture != "x86_64" || software[1].Architecture != "x86" {
		t.Fatalf("unexpected software inventory: %#v", software)
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
	if _, err := parseSoftware([]byte(`{"name":"Tool","architecture":"64-bit","registry_key":"tool","unexpected":true}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatal("software accepted an unknown field")
	}
	if _, err := parseSoftware([]byte("{\"name\":\"Tool\",\"architecture\":\"64-bit\",\"registry_key\":\"tool\"}\n{\"name\":\"Tool duplicate\",\"architecture\":\"64-bit\",\"registry_key\":\"TOOL\"}\n")); err == nil {
		t.Fatal("software accepted a duplicate registry identity")
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
