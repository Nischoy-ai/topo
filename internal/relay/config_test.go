package relay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAndExecuteLocalProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "relay.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version":"v1alpha1",
  "relay_id":"relay-1",
  "site_id":"site-1",
  "profiles":[{"id":"this-host","plugin":"local"}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := (Executor{Config: config}).Execute(t.Context(), Job{JobID: "job-1", Type: JobTypeDiscover, ProfileID: "this-host"})
	if err != nil {
		t.Fatal(err)
	}
	if observation.JobID != "job-1" || observation.CollectorID != "relay-1" || len(observation.Assets) == 0 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestLoadConfigRejectsUnknownAndUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	tests := []string{
		`{"schema_version":"v1alpha1","relay_id":"relay","site_id":"site","profiles":[{"id":"p","plugin":"local","targets_file":"/tmp/targets"}]}`,
		`{"schema_version":"v1alpha1","relay_id":"relay","site_id":"site","profiles":[{"id":"p","plugin":"ssh-linux","targets_file":"targets","ssh":{"known_hosts_file":"/tmp/known","password_ref":"env:P"}}]}`,
		`{"schema_version":"v1alpha1","relay_id":"relay","site_id":"site","profiles":[{"id":"p","plugin":"future"}]}`,
		`{"schema_version":"v1alpha1","relay_id":"relay","site_id":"site","profiles":[{"id":"p","plugin":"local","command":"id"}]}`,
	}
	for index, body := range tests {
		path := filepath.Join(t.TempDir(), "relay.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("case %d succeeded", index)
		}
	}
}

func TestExecutorRejectsUnknownProfile(t *testing.T) {
	t.Parallel()
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay", SiteID: "site", Profiles: []ProfileConfig{{ID: "local", Plugin: "local"}}}
	if _, err := (Executor{Config: config}).Execute(t.Context(), Job{JobID: "job", ProfileID: "missing"}); err == nil {
		t.Fatal("Execute accepted unknown profile")
	}
}
