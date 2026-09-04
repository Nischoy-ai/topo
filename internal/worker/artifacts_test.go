package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type controlPlaneManifest struct {
	Roles           []string `json:"roles"`
	ScriptedRESTAPI struct {
		APIID        string `json:"api_id"`
		BasePath     string `json:"base_path"`
		RequiredRole string `json:"required_role"`
		Resources    []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Script string `json:"script"`
		} `json:"resources"`
		OAuthPolicy map[string]bool `json:"oauth_policy"`
	} `json:"scripted_rest_api"`
	Tables []struct {
		Name          string     `json:"name"`
		Fields        [][]any    `json:"fields"`
		UniqueIndexes [][]string `json:"unique_indexes"`
		Indexes       [][]string `json:"indexes"`
	} `json:"tables"`
	TableACLs map[string]map[string][]string `json:"table_acls"`
}

type controlPlaneSDKPackage struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type controlPlaneSDKConfig struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scopeId"`
	Name    string `json:"name"`
}

func TestControlPlaneManifestHasBoundedPassword2SSHSurface(t *testing.T) {
	directory := controlPlaneDirectory(t)
	body, err := os.ReadFile(filepath.Join(directory, "application.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest controlPlaneManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ScriptedRESTAPI.APIID != "tasks" || manifest.ScriptedRESTAPI.BasePath != "/api/x_664635_topo/v1/tasks" || manifest.ScriptedRESTAPI.RequiredRole != "x_664635_topo.worker" {
		t.Fatalf("unexpected worker API boundary: %#v", manifest.ScriptedRESTAPI)
	}
	wantResources := []string{
		"POST /claim",
		"POST /workers/heartbeat",
		"POST /workers/register",
		"POST /{id}/complete",
		"POST /{id}/credential",
		"POST /{id}/renew",
		"POST /{id}/results",
	}
	var gotResources []string
	for _, resource := range manifest.ScriptedRESTAPI.Resources {
		gotResources = append(gotResources, resource.Method+" "+resource.Path)
		if _, err := os.Stat(filepath.Join(directory, resource.Script)); err != nil {
			t.Fatalf("resource %q script is missing: %v", resource.Path, err)
		}
	}
	sort.Strings(gotResources)
	if strings.Join(gotResources, "\n") != strings.Join(wantResources, "\n") {
		t.Fatalf("worker resources = %q, want %q", gotResources, wantResources)
	}
	for _, name := range []string{"grant_only_scripted_resources", "deny_table_api", "deny_cmdb_api", "deny_ire_api"} {
		if !manifest.ScriptedRESTAPI.OAuthPolicy[name] {
			t.Fatalf("worker OAuth policy %q is not fail-closed", name)
		}
	}

	wantTables := map[string]bool{
		"x_664635_topo_credential_access":  false,
		"x_664635_topo_credential_binding": false,
		"x_664635_topo_worker_pool":        false,
		"x_664635_topo_worker":             false,
		"x_664635_topo_target_scope":       false,
		"x_664635_topo_profile":            false,
		"x_664635_topo_schedule":           false,
		"x_664635_topo_run":                false,
		"x_664635_topo_task":               false,
		"x_664635_topo_result":             false,
		"x_664635_topo_ire_delivery":       false,
		"x_664635_topo_ssh_credential":     false,
	}
	for _, table := range manifest.Tables {
		if _, expected := wantTables[table.Name]; !expected {
			t.Fatalf("unexpected Slice B table %q", table.Name)
		}
		wantTables[table.Name] = true
		if permissions, ok := manifest.TableACLs[table.Name]; !ok {
			t.Fatalf("table %q has no explicit ACL matrix", table.Name)
		} else {
			for operation, roles := range permissions {
				for _, role := range roles {
					if role == "worker" || role == "x_664635_topo.worker" {
						t.Fatalf("worker role received generic %s access to %s", operation, table.Name)
					}
				}
			}
		}
	}
	for table, found := range wantTables {
		if !found {
			t.Fatalf("required Slice B table %q is missing", table)
		}
	}
	assertManifestIndex(t, manifest, "x_664635_topo_target_scope", []string{"u_scope_id", "u_revision"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_ssh_credential", []string{"u_credential_id"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_credential_binding", []string{"u_binding_id", "u_revision"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_credential_access", []string{"u_event_id"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_task", []string{"u_worker_pool", "u_state", "u_partition_ordinal", "sys_created_on"}, false)
	assertManifestIndex(t, manifest, "x_664635_topo_task", []string{"u_run", "u_partition_key"}, false)
	assertManifestIndex(t, manifest, "x_664635_topo_task", []string{"u_state", "u_lease_expires"}, false)
	assertManifestIndex(t, manifest, "x_664635_topo_task", []string{"u_pool_lease_slot"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_task", []string{"u_worker_lease_slot"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_result", []string{"u_task", "u_attempt_id", "u_chunk_number"}, true)
	assertManifestIndex(t, manifest, "x_664635_topo_ire_delivery", []string{"u_task", "u_attempt_id"}, true)

	manifestText := string(body)
	for _, deferred := range []string{"vault:", "k8s:", "external_vault"} {
		if strings.Contains(manifestText, deferred) {
			t.Fatalf("Slice B manifest expanded into deferred credential work %q", deferred)
		}
	}
	if strings.Contains(manifestText, "u_lease_token\"") {
		t.Fatal("manifest stores a raw lease token instead of only its digest")
	}
}

func TestControlPlaneScriptsEnforceReviewedBoundary(t *testing.T) {
	directory := controlPlaneDirectory(t)
	entries, err := filepath.Glob(filepath.Join(directory, "scripts", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(body)
		all.WriteByte('\n')
	}
	scripts := all.String()
	for _, required := range []string{
		"cas.addQuery('u_state', 'ready')",
		"cas.updateMultiple()",
		"new GlideDigest().getSHA256Hex",
		"identifyCIEnhanced('Nischoy Topo'",
		"createOrUpdateCIEnhanced('Nischoy Topo'",
		"cmdb_ci_computer",
		"cmdb_ci_network_adapter",
		"Owns::Owned by",
		"new GlideSysAttachment().deleteAttachment",
		"compileTargetScope",
		"u_cancel_requested",
		"u_max_leases",
		"u_pool_lease_slot",
		"u_worker_lease_slot",
		"getDecryptedValue()",
		"_recordCredentialAccess",
		"ssh_linux.v1",
		"validated SSH no-data observation",
	} {
		if !strings.Contains(scripts, required) {
			t.Fatalf("control-plane scripts do not contain required invariant %q", required)
		}
	}
	lower := strings.ToLower(scripts)
	for _, forbidden := range []string{
		"ecc_queue",
		"ecc_agent",
		"cmdb_rel_ci",
		"new gliderecord('cmdb_ci",
		"new gliderecord(\"cmdb_ci",
		"powershell",
		"wql",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("control-plane scripts contain forbidden surface %q", forbidden)
		}
	}
	if strings.Contains(scripts, "setValue('u_lease_token',") || strings.Contains(scripts, ".u_lease_token =") {
		t.Fatal("control-plane scripts persist a raw lease token")
	}
}

func TestControlPlaneFluentPackageIsAuthoritativeAndBuildable(t *testing.T) {
	directory := controlPlaneDirectory(t)
	packageBody, err := os.ReadFile(filepath.Join(directory, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageConfig controlPlaneSDKPackage
	if err := json.Unmarshal(packageBody, &packageConfig); err != nil {
		t.Fatal(err)
	}
	if packageConfig.Name != "@nischoy/topo-servicenow-control-plane" || packageConfig.Version != "0.4.4" {
		t.Fatalf("unexpected Fluent package identity: %#v", packageConfig)
	}
	if packageConfig.DevDependencies["@servicenow/sdk"] != "4.9.0" {
		t.Fatalf("ServiceNow SDK is not exactly pinned: %#v", packageConfig.DevDependencies)
	}
	if _, err := os.Stat(filepath.Join(directory, "package-lock.json")); err != nil {
		t.Fatalf("Fluent package lock is missing: %v", err)
	}

	configBody, err := os.ReadFile(filepath.Join(directory, "now.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sdkConfig controlPlaneSDKConfig
	if err := json.Unmarshal(configBody, &sdkConfig); err != nil {
		t.Fatal(err)
	}
	if sdkConfig.Name != "Nischoy Topo" || sdkConfig.Scope != "x_664635_topo" || len(sdkConfig.ScopeID) != 32 {
		t.Fatalf("unexpected Fluent scope configuration: %#v", sdkConfig)
	}

	fluentFiles, err := filepath.Glob(filepath.Join(directory, "src", "fluent", "*.now.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fluentFiles) < 4 {
		t.Fatalf("Fluent application has %d metadata files, want at least four", len(fluentFiles))
	}
	var fluent strings.Builder
	for _, path := range fluentFiles {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fluent.Write(body)
		fluent.WriteByte('\n')
	}
	source := fluent.String()
	for _, required := range []string{
		"RestApi({",
		"serviceId: 'tasks'",
		"rest-api-topo-worker-version-v1",
		"ScheduledScript({",
		"BusinessRule({",
		"ScriptInclude({",
		"UiAction({",
		"application: topoMenu",
		"executionStart: '2026-01-01 00:00:00'",
		"CrossScopePrivilege({",
		"allowWebServiceAccess: false",
		"name: 'x_664635_topo.worker'",
		"name: 'x_664635_topo.credential_admin'",
		"Password2Column({",
		"path: '/{id}/credential'",
		"targetName: 'sn_cmdb.IdentificationEngine'",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Fluent sources do not contain required metadata boundary %q", required)
		}
	}
	if strings.Contains(source, "Now.ref('sys_app_application'") {
		t.Fatal("Fluent navigation uses a build-variant application lookup instead of the stable menu record")
	}
	if strings.Count(source, "ScheduledScript({") != 2 {
		t.Fatalf("Fluent sources define %d scheduled scripts, want two", strings.Count(source, "ScheduledScript({"))
	}

	manifestBody, err := os.ReadFile(filepath.Join(directory, "application.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest controlPlaneManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, table := range manifest.Tables {
		if !strings.Contains(source, "name: '"+table.Name+"'") {
			t.Fatalf("review manifest table %q has no Fluent definition", table.Name)
		}
	}
}

func TestWorkerProductionPackageHasNoDurableStateOrListener(t *testing.T) {
	directory := filepath.Dir(currentFile(t))
	entries, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var production strings.Builder
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		production.Write(body)
		production.WriteByte('\n')
	}
	source := production.String()
	for _, forbidden := range []string{
		"database/sql",
		"internal/store",
		"modernc.org/sqlite",
		"os.Create(",
		"os.OpenFile(",
		"os.WriteFile(",
		"net.Listen(",
		"http.ListenAndServe(",
		"spool",
		"journal",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("stateless worker production package contains forbidden state/listener surface %q", forbidden)
		}
	}
}

func assertManifestIndex(t *testing.T, manifest controlPlaneManifest, tableName string, want []string, unique bool) {
	t.Helper()
	for _, table := range manifest.Tables {
		if table.Name != tableName {
			continue
		}
		indexes := table.Indexes
		if unique {
			indexes = table.UniqueIndexes
		}
		for _, index := range indexes {
			if strings.Join(index, "\x00") == strings.Join(want, "\x00") {
				return
			}
		}
	}
	t.Fatalf("table %q is missing %s index %v", tableName, map[bool]string{true: "unique", false: "claim/lookup"}[unique], want)
}

func controlPlaneDirectory(t *testing.T) string {
	t.Helper()
	file := currentFile(t)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "integrations", "servicenow", "topo-control-plane"))
}

func currentFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate worker artifact test")
	}
	return file
}
