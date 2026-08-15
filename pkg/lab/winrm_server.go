package lab

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery/winrm"
)

const (
	LabWinRMUsername = "topo-lab"
	LabWinRMPassword = "topo-lab"
	maxSOAPBodyBytes = 1 << 20
)

// WinRMServer exposes Windows personas through real WS-Management SOAP
// Enumerate/Pull exchanges. It routes only the production plugin's exact
// audited operation contract.
type WinRMServer struct {
	Estate Estate
	mu     sync.Mutex
	scans  map[string]int
}

func NewWinRMServer(estate Estate) *WinRMServer {
	return &WinRMServer{Estate: estate, scans: map[string]int{}}
}

func (server *WinRMServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /wsman/{id}", server.serveWSMan)
	return mux
}

func (server *WinRMServer) serveWSMan(response http.ResponseWriter, request *http.Request) {
	host, ok := server.host(request.PathValue("id"))
	if !ok || host.OS != "windows" {
		http.Error(response, "unknown Windows host", http.StatusNotFound)
		return
	}
	username, password, authenticated := request.BasicAuth()
	if !authenticated || username != LabWinRMUsername || password != LabWinRMPassword || host.Fault == "auth_failure" {
		response.Header().Set("WWW-Authenticate", `Basic realm="Topo Lab WinRM"`)
		http.Error(response, "authentication rejected", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxSOAPBodyBytes+1))
	if err != nil || len(body) > maxSOAPBodyBytes {
		http.Error(response, "invalid SOAP request body", http.StatusRequestEntityTooLarge)
		return
	}
	soapRequest, err := winrm.ParseSOAPRequest(body)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	if soapRequest.Action == winrm.ActionEnumerate {
		if soapRequest.BodyOperation != "Enumerate" || (soapRequest.Query != "" && soapRequest.FilterDialect != winrm.DialectWQL) {
			http.Error(response, "invalid audited enumeration envelope", http.StatusBadRequest)
			return
		}
		operation, allowed := winrm.MatchOperation(soapRequest.Action, soapRequest.ResourceURI, soapRequest.Query)
		if !allowed {
			http.Error(response, "operation rejected by Topo Lab allowlist", http.StatusBadRequest)
			return
		}
		if operation.Name == winrm.OperationComputerSystem {
			server.mu.Lock()
			server.scans[host.ID]++
			server.mu.Unlock()
		}
		if server.unavailable(host) {
			http.Error(response, "simulated host disappeared", http.StatusGone)
			return
		}
		if !server.wait(request, host) {
			return
		}
		writeSOAP(response, enumerateResponse(enumerationContext(host.ID, operation.Name)))
		return
	}

	if soapRequest.Action != winrm.ActionPull {
		http.Error(response, "operation rejected by Topo Lab allowlist", http.StatusBadRequest)
		return
	}
	if soapRequest.BodyOperation != "Pull" || soapRequest.FilterDialect != "" {
		http.Error(response, "invalid pull envelope", http.StatusBadRequest)
		return
	}
	hostID, operationName, ok := parseEnumerationContext(soapRequest.EnumerationContext)
	operation, operationOK := operationForName(operationName)
	if !ok || !operationOK || hostID != host.ID || operation.ResourceURI != soapRequest.ResourceURI || soapRequest.Query != "" {
		http.Error(response, "invalid enumeration context", http.StatusBadRequest)
		return
	}
	if server.unavailable(host) {
		http.Error(response, "simulated host disappeared", http.StatusGone)
		return
	}
	if !server.wait(request, host) {
		return
	}
	if host.Fault == "permission_denied" && !operation.Required {
		writeSOAPFault(response, http.StatusForbidden, "access denied for optional CIM class")
		return
	}
	if host.Fault == "malformed" && operation.Required && operation.Name == winrm.OperationComputerSystemProduct {
		response.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
		_, _ = io.WriteString(response, `<s:Envelope><broken>`)
		return
	}
	writeSOAP(response, pullResponse(operation, host))
}

func (server *WinRMServer) wait(request *http.Request, host Host) bool {
	if host.Fault == "timeout" {
		<-request.Context().Done()
		return false
	}
	if host.LatencyMS > 0 {
		select {
		case <-request.Context().Done():
			return false
		case <-time.After(time.Duration(host.LatencyMS) * time.Millisecond):
		}
	}
	return true
}

func (server *WinRMServer) unavailable(host Host) bool {
	if host.Fault != "disappear" {
		return false
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.scans[host.ID] > 1
}

func (server *WinRMServer) host(id string) (Host, bool) {
	for _, host := range server.Estate.Hosts {
		if host.ID == id {
			return host, true
		}
	}
	return Host{}, false
}

func operationForName(name string) (winrm.Operation, bool) {
	for _, operation := range winrm.AuditedOperations() {
		if operation.Name == name {
			return operation, true
		}
	}
	return winrm.Operation{}, false
}

func enumerateResponse(context string) string {
	return soapResponse(winrm.ActionEnumerate+"Response", `<n:EnumerateResponse><n:EnumerationContext>`+escapeXML(context)+`</n:EnumerationContext></n:EnumerateResponse>`)
}

func pullResponse(operation winrm.Operation, host Host) string {
	resource := operation.ResourceURI
	objects := []string{}
	appendObject := func(write func(*strings.Builder)) {
		var object strings.Builder
		object.WriteString(`<p:` + resourceClass(resource) + ` xmlns:p="` + resource + `">`)
		write(&object)
		object.WriteString(`</p:` + resourceClass(resource) + `>`)
		objects = append(objects, object.String())
	}
	switch operation.Name {
	case winrm.OperationComputerSystem:
		appendObject(func(object *strings.Builder) {
			writeProperty(object, "Name", host.Name)
			writeProperty(object, "Domain", "WORKGROUP")
			writeProperty(object, "PartOfDomain", "false")
			writeProperty(object, "Manufacturer", "Topo Lab")
			writeProperty(object, "Model", host.Persona)
			writeProperty(object, "NumberOfLogicalProcessors", strconv.Itoa(host.CPUCount))
			writeProperty(object, "TotalPhysicalMemory", strconv.Itoa(host.MemoryMB*1024*1024))
		})
	case winrm.OperationComputerSystemProduct:
		appendObject(func(object *strings.Builder) { writeProperty(object, "UUID", host.MachineID) })
	case winrm.OperationBIOS:
		appendObject(func(object *strings.Builder) { writeProperty(object, "SerialNumber", host.Serial) })
	case winrm.OperationOperatingSystem:
		appendObject(func(object *strings.Builder) {
			writeProperty(object, "Caption", host.OSVersion)
			version, build := windowsVersion(host.Persona)
			writeProperty(object, "Version", version)
			writeProperty(object, "BuildNumber", build)
			writeProperty(object, "OSArchitecture", "64-bit")
		})
	case winrm.OperationNetwork:
		appendObject(func(object *strings.Builder) {
			writeProperty(object, "Description", "primary")
			writeProperty(object, "InterfaceIndex", "1")
			writeProperty(object, "MACAddress", host.MACAddress)
			writeArrayProperty(object, "IPAddress", []string{host.IPAddress})
			writeArrayProperty(object, "IPSubnet", []string{"24"})
		})
	case winrm.OperationVolumes:
		appendObject(func(object *strings.Builder) {
			writeProperty(object, "DeviceID", "C:")
			writeProperty(object, "VolumeName", "System")
			writeProperty(object, "FileSystem", "NTFS")
			writeProperty(object, "Size", "137438953472")
			writeProperty(object, "FreeSpace", "68719476736")
		})
	case winrm.OperationServices:
		for _, service := range host.Services {
			service := service
			appendObject(func(object *strings.Builder) {
				writeProperty(object, "Name", service)
				writeProperty(object, "DisplayName", service)
				writeProperty(object, "State", "Running")
				writeProperty(object, "StartMode", "Auto")
				writeProperty(object, "StartName", "LocalSystem")
			})
		}
	case winrm.OperationPatches:
		for _, patch := range windowsPatches(host.Persona) {
			patch := patch
			appendObject(func(object *strings.Builder) {
				writeProperty(object, "HotFixID", patch)
				writeProperty(object, "Description", "Security Update")
				writeProperty(object, "InstalledOn", "8/1/2026")
			})
		}
	}
	body := `<n:PullResponse><n:Items>` + strings.Join(objects, "") + `</n:Items><n:EndOfSequence/></n:PullResponse>`
	return soapResponse(winrm.ActionPull+"Response", body)
}

func soapResponse(action, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:n="http://schemas.xmlsoap.org/ws/2004/09/enumeration">` +
		`<s:Header><a:Action>` + action + `</a:Action></s:Header><s:Body>` + body + `</s:Body></s:Envelope>`
}

func writeSOAP(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
	response.Header().Set("X-Topo-Lab-Protocol", ScenarioVersion)
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, body)
}

func writeSOAPFault(response http.ResponseWriter, status int, reason string) {
	body := `<s:Fault><s:Code><s:Value>s:Sender</s:Value></s:Code><s:Reason><s:Text xml:lang="en-US">` + escapeXML(reason) + `</s:Text></s:Reason></s:Fault>`
	response.Header().Set("Content-Type", "application/soap+xml;charset=UTF-8")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, soapResponse("http://www.w3.org/2005/08/addressing/fault", body))
}

func writeProperty(builder *strings.Builder, name, value string) {
	builder.WriteString(`<p:` + name + `>` + escapeXML(value) + `</p:` + name + `>`)
}

func writeArrayProperty(builder *strings.Builder, name string, values []string) {
	builder.WriteString(`<p:` + name + `>`)
	for _, value := range values {
		builder.WriteString(`<p:string>` + escapeXML(value) + `</p:string>`)
	}
	builder.WriteString(`</p:` + name + `>`)
}

func escapeXML(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func resourceClass(resource string) string {
	if index := strings.LastIndexByte(resource, '/'); index >= 0 {
		return resource[index+1:]
	}
	return resource
}

func enumerationContext(hostID, operation string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(hostID + "\x00" + operation))
}

func parseEnumerationContext(value string) (string, string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func windowsVersion(persona string) (string, string) {
	switch persona {
	case "windows-2019":
		return "10.0.17763", "17763"
	case "windows-2022":
		return "10.0.20348", "20348"
	case "windows-2025":
		return "10.0.26100", "26100"
	default:
		return "10.0.0", "0"
	}
}

func windowsPatches(persona string) []string {
	switch persona {
	case "windows-2019":
		return []string{"KB9000001", "KB9000002"}
	case "windows-2022":
		return []string{"KB9000003", "KB9000004"}
	case "windows-2025":
		return []string{"KB9000005", "KB9000006"}
	default:
		return nil
	}
}

func WinRMTargetURL(addr, hostID string) string {
	return fmt.Sprintf("%s/wsman/%s", ParseListenURL(addr), hostID)
}
