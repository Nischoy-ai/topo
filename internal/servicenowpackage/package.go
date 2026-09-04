// Package servicenowpackage validates and normalizes the installable ZIP
// emitted by ServiceNow's pinned SDK for Topo's authoritative Fluent app.
package servicenowpackage

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	Scope        = "x_664635_topo"
	AppVersion   = "0.4.4"
	SDKVersion   = "4.9.0"
	ArtifactName = "nischoy_topo_servicenow_control_plane_0_4_4.zip"

	maxArchiveBytes = 64 << 20
	maxEntryBytes   = 8 << 20
	maxEntries      = 4096
)

var fixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	bomSerialPattern    = regexp.MustCompile(`"serialNumber": "urn:uuid:[0-9a-fA-F-]{36}"`)
	bomTimestampPattern = regexp.MustCompile(`"timestamp": "[0-9TZ:.-]+"`)
)

var expectedTables = []string{
	Scope + "_credential_access",
	Scope + "_credential_binding",
	Scope + "_ire_delivery",
	Scope + "_profile",
	Scope + "_result",
	Scope + "_run",
	Scope + "_schedule",
	Scope + "_ssh_credential",
	Scope + "_target_scope",
	Scope + "_task",
	Scope + "_worker",
	Scope + "_worker_pool",
}

var expectedRoles = []string{
	Scope + ".admin",
	Scope + ".credential_admin",
	Scope + ".operator",
	Scope + ".viewer",
	Scope + ".worker",
}

var expectedResources = []string{
	"POST /claim",
	"POST /workers/heartbeat",
	"POST /workers/register",
	"POST /{id}/complete",
	"POST /{id}/credential",
	"POST /{id}/renew",
	"POST /{id}/results",
}

// Metadata is the bounded release description emitted after validation.
type Metadata struct {
	SchemaVersion int      `json:"schema_version"`
	Scope         string   `json:"scope"`
	AppVersion    string   `json:"app_version"`
	SDKVersion    string   `json:"sdk_version"`
	Artifact      string   `json:"artifact"`
	SHA256        string   `json:"sha256"`
	Entries       int      `json:"entries"`
	Tables        []string `json:"tables"`
	Roles         []string `json:"roles"`
	Resources     []string `json:"worker_resources"`
}

type entry struct {
	name string
	body []byte
}

// Normalize validates input and writes a byte-reproducible ZIP to output.
// Output must not already exist.
func Normalize(input, output string) (Metadata, error) {
	entries, err := readArchive(input)
	if err != nil {
		return Metadata{}, err
	}
	if err := validateInventory(entries); err != nil {
		return Metadata{}, err
	}
	if err := normalizeGeneratedBOM(entries); err != nil {
		return Metadata{}, err
	}
	rewriteInventory(entries)
	if err := validateContract(entries); err != nil {
		return Metadata{}, err
	}
	if err := writeArchive(output, entries); err != nil {
		return Metadata{}, err
	}
	digest, err := fileDigest(output)
	if err != nil {
		_ = os.Remove(output)
		return Metadata{}, err
	}
	return Metadata{
		SchemaVersion: 1,
		Scope:         Scope,
		AppVersion:    AppVersion,
		SDKVersion:    SDKVersion,
		Artifact:      ArtifactName,
		SHA256:        digest,
		Entries:       len(entries),
		Tables:        append([]string(nil), expectedTables...),
		Roles:         append([]string(nil), expectedRoles...),
		Resources:     append([]string(nil), expectedResources...),
	}, nil
}

// normalizeGeneratedBOM removes the only two nondeterministic content values
// emitted by SDK 4.9.0 after Fluent itself has produced stable metadata. The
// raw SDK inventory is verified before this function runs, and a new inventory
// is generated afterward. No application record, script, ACL, table, or route
// content is changed.
func normalizeGeneratedBOM(entries map[string]entry) error {
	wantPath := "<path>" + Scope + "/@nischoy/topo-servicenow-control-plane/" + AppVersion + "/bom.json</path>"
	var bomName string
	for name, item := range entries {
		if strings.HasPrefix(name, "update/sys_module_") && bytes.Contains(item.body, []byte(wantPath)) {
			if bomName != "" {
				return errors.New("ServiceNow app package contains multiple generated BOM records")
			}
			bomName = name
		}
	}
	if bomName == "" {
		return errors.New("ServiceNow app package omits the generated BOM record")
	}
	item := entries[bomName]
	if len(bomSerialPattern.FindAll(item.body, 2)) != 1 || len(bomTimestampPattern.FindAll(item.body, 2)) != 1 {
		return errors.New("ServiceNow generated BOM has an unexpected serial or timestamp contract")
	}
	serial := `"serialNumber": "urn:uuid:` + deterministicBOMUUID() + `"`
	item.body = bomSerialPattern.ReplaceAll(item.body, []byte(serial))
	item.body = bomTimestampPattern.ReplaceAll(item.body, []byte(`"timestamp": "1980-01-01T00:00:00.000Z"`))
	entries[bomName] = item
	return nil
}

func deterministicBOMUUID() string {
	digest := sha256.Sum256([]byte(Scope + ":" + AppVersion + ":bom"))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	value := hex.EncodeToString(digest[:16])
	return value[0:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32]
}

func rewriteInventory(entries map[string]entry) {
	var names []string
	for name := range entries {
		if name != "package_inventory.csv" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var body strings.Builder
	fmt.Fprintf(&body, "#build=%s\n#appVersion=%s\n#type=scoped\n#version=1.0.0\n", Scope, AppVersion)
	for _, name := range names {
		digest := sha256.Sum256(entries[name].body)
		fmt.Fprintf(&body, "%s;%s\n", name, hex.EncodeToString(digest[:]))
	}
	entries["package_inventory.csv"] = entry{name: "package_inventory.csv", body: []byte(body.String())}
}

func readArchive(filename string) (map[string]entry, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("inspect ServiceNow app package: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxArchiveBytes {
		return nil, errors.New("ServiceNow app package must be a non-empty bounded regular file")
	}
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open ServiceNow app package: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxEntries {
		return nil, fmt.Errorf("ServiceNow app package must contain 1-%d entries", maxEntries)
	}
	entries := make(map[string]entry, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		name, err := safeName(file.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := entries[name]; exists {
			return nil, fmt.Errorf("ServiceNow app package has duplicate entry %q", name)
		}
		if file.FileInfo().IsDir() || file.UncompressedSize64 > maxEntryBytes || total+file.UncompressedSize64 > maxArchiveBytes {
			return nil, fmt.Errorf("ServiceNow app package entry %q exceeds bounds", name)
		}
		stream, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open ServiceNow app package entry %q: %w", name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, maxEntryBytes+1))
		closeErr := stream.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read ServiceNow app package entry %q: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close ServiceNow app package entry %q: %w", name, closeErr)
		}
		if len(body) > maxEntryBytes || uint64(len(body)) != file.UncompressedSize64 {
			return nil, fmt.Errorf("ServiceNow app package entry %q has an invalid size", name)
		}
		total += uint64(len(body))
		entries[name] = entry{name: name, body: body}
	}
	return entries, nil
}

func safeName(value string) (string, error) {
	if strings.Contains(value, "\\") || strings.Count(value, "\x00") != 0 {
		return "", fmt.Errorf("unsafe ServiceNow app package entry %q", value)
	}
	name := strings.TrimPrefix(value, "/")
	if name == "" || strings.HasPrefix(name, "/") || pathpkg.Clean(name) != name || strings.HasPrefix(name, "../") || strings.HasSuffix(name, "/") {
		return "", fmt.Errorf("unsafe ServiceNow app package entry %q", value)
	}
	return name, nil
}

func validateInventory(entries map[string]entry) error {
	inventory, ok := entries["package_inventory.csv"]
	if !ok {
		return errors.New("ServiceNow app package omits package_inventory.csv")
	}
	headers := make(map[string]string)
	listed := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(inventory.body))
	scanner.Buffer(make([]byte, 4096), maxEntryBytes)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, "#") {
			parts := strings.SplitN(strings.TrimPrefix(line, "#"), "=", 2)
			if len(parts) != 2 || parts[0] == "" || headers[parts[0]] != "" {
				return errors.New("ServiceNow package inventory has an invalid header")
			}
			headers[parts[0]] = parts[1]
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) != 2 {
			return errors.New("ServiceNow package inventory has an invalid entry")
		}
		name, err := safeName(parts[0])
		if err != nil || name == "package_inventory.csv" || listed[name] {
			return errors.New("ServiceNow package inventory has an unsafe or duplicate entry")
		}
		if len(parts[1]) != 64 {
			return errors.New("ServiceNow package inventory has an invalid digest")
		}
		if _, err := hex.DecodeString(parts[1]); err != nil {
			return errors.New("ServiceNow package inventory has an invalid digest")
		}
		item, found := entries[name]
		if !found {
			return fmt.Errorf("ServiceNow package inventory references missing entry %q", name)
		}
		digest := sha256.Sum256(item.body)
		if hex.EncodeToString(digest[:]) != strings.ToLower(parts[1]) {
			return fmt.Errorf("ServiceNow package inventory digest mismatch for %q", name)
		}
		listed[name] = true
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read ServiceNow package inventory: %w", err)
	}
	for key, want := range map[string]string{"build": Scope, "appVersion": AppVersion, "type": "scoped", "version": "1.0.0"} {
		if headers[key] != want {
			return fmt.Errorf("ServiceNow package inventory %s = %q, want %q", key, headers[key], want)
		}
	}
	if len(listed) != len(entries)-1 {
		return errors.New("ServiceNow package inventory does not cover every entry")
	}
	return nil
}

func validateContract(entries map[string]entry) error {
	var tables []string
	var roles []string
	var resources []string
	var scopeFiles []entry
	for name, item := range entries {
		switch {
		case strings.HasPrefix(name, "dictionary/") && strings.HasSuffix(name, ".xml"):
			tables = append(tables, strings.TrimSuffix(strings.TrimPrefix(name, "dictionary/"), ".xml"))
		case strings.HasPrefix(name, "update/sys_user_role_") && !strings.HasPrefix(name, "update/sys_user_role_contains_"):
			values, err := xmlFields(item.body, "name")
			if err != nil || values["name"] == "" {
				return fmt.Errorf("parse ServiceNow role %q", name)
			}
			roles = append(roles, values["name"])
		case strings.HasPrefix(name, "update/sys_ws_operation_"):
			values, err := xmlFields(item.body, "active", "http_method", "relative_path")
			if err != nil || values["active"] != "true" || values["http_method"] == "" || values["relative_path"] == "" {
				return fmt.Errorf("parse active ServiceNow worker resource %q", name)
			}
			resources = append(resources, values["http_method"]+" "+values["relative_path"])
		case strings.HasPrefix(name, "scope/sys_app_"):
			scopeFiles = append(scopeFiles, item)
		}
	}
	sort.Strings(tables)
	sort.Strings(roles)
	sort.Strings(resources)
	if !equalStrings(tables, expectedTables) {
		return fmt.Errorf("ServiceNow app tables = %v, want %v", tables, expectedTables)
	}
	if !equalStrings(roles, expectedRoles) {
		return fmt.Errorf("ServiceNow app roles = %v, want %v", roles, expectedRoles)
	}
	if !equalStrings(resources, expectedResources) {
		return fmt.Errorf("ServiceNow worker resources = %v, want %v", resources, expectedResources)
	}
	if len(scopeFiles) != 1 {
		return errors.New("ServiceNow app package must contain exactly one scope record")
	}
	values, err := xmlFields(scopeFiles[0].body, "name", "scope", "version")
	if err != nil {
		return fmt.Errorf("parse ServiceNow scope record: %w", err)
	}
	if values["name"] != "Nischoy Topo" || values["scope"] != Scope || values["version"] != AppVersion {
		return fmt.Errorf("unexpected ServiceNow app identity name=%q scope=%q version=%q", values["name"], values["scope"], values["version"])
	}
	return nil
}

func xmlFields(body []byte, names ...string) (map[string]string, error) {
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	values := make(map[string]string, len(names))
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || !wanted[start.Name.Local] || values[start.Name.Local] != "" {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		values[start.Name.Local] = strings.TrimSpace(value)
	}
}

func writeArchive(filename string, entries map[string]entry) (err error) {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create normalized ServiceNow app package: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = file.Close()
			_ = os.Remove(filename)
		}
	}()
	writer := zip.NewWriter(file)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: "/" + name, Method: zip.Deflate}
		header.SetModTime(fixedTime)
		header.SetMode(0o644)
		stream, err := writer.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create normalized ServiceNow app entry %q: %w", name, err)
		}
		if _, err := stream.Write(entries[name].body); err != nil {
			return fmt.Errorf("write normalized ServiceNow app entry %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close normalized ServiceNow app package: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("publish normalized ServiceNow app package: %w", err)
	}
	completed = true
	return nil
}

func fileDigest(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxArchiveBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
