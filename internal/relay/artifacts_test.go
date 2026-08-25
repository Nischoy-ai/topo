package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceNowApplicationArtifactsMatchControlContract(t *testing.T) {
	t.Parallel()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	appDir := filepath.Join(root, "integrations", "servicenow", "topo-relay")
	manifestBody, err := os.ReadFile(filepath.Join(appDir, "application.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Scope string `json:"scope"`
		API   struct {
			BasePath string `json:"base_path"`
		} `json:"scripted_rest_api"`
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Scope != "x_nischoy_topo" || manifest.API.BasePath != "/api/x_nischoy_topo/v1" {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, name := range []string{"poll.js", "result.js", "enqueue_due_schedules.js", "start_discovery.js"} {
		body, err := os.ReadFile(filepath.Join(appDir, "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if strings.Contains(text, "eval(") || strings.Contains(text, "cmdb_ci") {
			t.Fatalf("%s contains prohibited dynamic execution or direct CMDB access", name)
		}
	}
	for _, name := range []string{"poll.js", "result.js"} {
		body, _ := os.ReadFile(filepath.Join(appDir, "scripts", name))
		if !strings.Contains(string(body), "gs.getUserID()") {
			t.Fatalf("%s does not bind requests to the authenticated Relay user", name)
		}
	}
}
