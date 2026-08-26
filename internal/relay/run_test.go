package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestServiceNowToLocalDiscoveryToIREToResult(t *testing.T) {
	t.Parallel()
	control, irePublisher, received := newServiceNowFixture(t, Job{JobID: "job-local", Type: JobTypeDiscover, ProfileID: "local"})
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay-1", SiteID: "pilot", Profiles: []ProfileConfig{{ID: "local", Plugin: "local"}}}
	spool, err := NewSpool(t.TempDir(), make([]byte, 32), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	cycle(t.Context(), RunConfig{
		FileConfig: config,
		Version:    "test",
		Control:    control,
		Executor:   Executor{Config: config},
		Publisher:  irePublisher,
		Spool:      spool,
		Logger:     discardLogger(),
	}, discardLogger())
	result := waitResult(t, received)
	if !result.Success || result.JobID != "job-local" || result.RelayID != "relay-1" || result.Assets == 0 {
		t.Fatalf("result = %#v", result)
	}
	if names, _ := spool.Pending(); len(names) != 0 {
		t.Fatalf("spool still contains %#v", names)
	}
}

func TestRelayRetriesIREFromEncryptedSpoolBeforeReporting(t *testing.T) {
	t.Parallel()
	control := &fakeControl{jobs: []Job{{JobID: "job-retry", Type: JobTypeDiscover, ProfileID: "local"}}}
	publish := &flakyPublisher{failures: 2}
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay-1", SiteID: "pilot", Profiles: []ProfileConfig{{ID: "local", Plugin: "local"}}}
	spool, err := NewSpool(t.TempDir(), make([]byte, 32), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	runConfig := RunConfig{FileConfig: config, Version: "test", Control: control, Executor: Executor{Config: config}, Publisher: publish, Spool: spool, Logger: discardLogger()}
	for range 3 {
		cycle(t.Context(), runConfig, discardLogger())
	}
	if publish.calls != 3 {
		t.Fatalf("publish calls = %d", publish.calls)
	}
	if len(control.results) != 1 || !control.results[0].Success {
		t.Fatalf("results = %#v", control.results)
	}
	if names, _ := spool.Pending(); len(names) != 0 {
		t.Fatalf("spool still contains %#v", names)
	}
}

func TestRelayRetriesResultWithoutRepublishingIRE(t *testing.T) {
	t.Parallel()
	control := &fakeControl{jobs: []Job{{JobID: "job-result-retry", Type: JobTypeDiscover, ProfileID: "local"}}, reportFailures: 1}
	publish := &flakyPublisher{}
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay-1", SiteID: "pilot", Profiles: []ProfileConfig{{ID: "local", Plugin: "local"}}}
	spool, err := NewSpool(t.TempDir(), make([]byte, 32), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	runConfig := RunConfig{FileConfig: config, Version: "test", Control: control, Executor: Executor{Config: config}, Publisher: publish, Spool: spool, Logger: discardLogger()}
	cycle(t.Context(), runConfig, discardLogger())
	cycle(t.Context(), runConfig, discardLogger())
	if publish.calls != 1 {
		t.Fatalf("publish calls = %d, want one despite result retry", publish.calls)
	}
	if control.reportCalls != 2 || len(control.results) != 1 || !control.results[0].Success {
		t.Fatalf("report calls=%d results=%#v", control.reportCalls, control.results)
	}
}

func TestUnknownProfileFailsWithoutPublishing(t *testing.T) {
	t.Parallel()
	control := &fakeControl{jobs: []Job{{JobID: "job-unknown", Type: JobTypeDiscover, ProfileID: "not-local"}}}
	publish := &flakyPublisher{}
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay-1", SiteID: "pilot", Profiles: []ProfileConfig{{ID: "local", Plugin: "local"}}}
	spool, err := NewSpool(t.TempDir(), make([]byte, 32), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	cycle(t.Context(), RunConfig{FileConfig: config, Control: control, Executor: Executor{Config: config}, Publisher: publish, Spool: spool, Logger: discardLogger()}, discardLogger())
	if publish.calls != 0 {
		t.Fatalf("publish calls = %d", publish.calls)
	}
	if len(control.results) != 1 || control.results[0].Success || control.results[0].Error == "" {
		t.Fatalf("results = %#v", control.results)
	}
}

func TestServiceNowToSSHNetworkDiscoveryToIREToResult(t *testing.T) {
	estate, err := lab.Generate(lab.DefaultScenario(1, 0, 42))
	if err != nil {
		t.Fatal(err)
	}
	sshServer, err := lab.NewSSHServer(estate)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = sshServer.Serve(listener) }()

	dir := t.TempDir()
	targetsPath := filepath.Join(dir, "targets.txt")
	knownHostsPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(targetsPath, []byte(estate.Hosts[0].ID+"@"+listener.Addr().String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{listener.Addr().String()}, sshServer.PublicKey())
	if err := os.WriteFile(knownHostsPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOPO_RELAY_TEST_SSH_PASSWORD", lab.LabSSHPassword)

	profile := ProfileConfig{ID: "linux-pilot", Plugin: "ssh-linux", TargetsFile: targetsPath, SSH: &SSHConfig{
		PasswordRef:    "env:TOPO_RELAY_TEST_SSH_PASSWORD",
		KnownHostsFile: knownHostsPath,
		Concurrency:    1,
		ConnectTimeout: "2s",
		CommandTimeout: "2s",
	}}
	config := FileConfig{SchemaVersion: ContractVersion, RelayID: "relay-ssh", SiteID: "pilot", Profiles: []ProfileConfig{profile}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	control, irePublisher, received := newServiceNowFixture(t, Job{JobID: "job-ssh", Type: JobTypeDiscover, ProfileID: profile.ID})
	spool, err := NewSpool(filepath.Join(dir, "spool"), make([]byte, 32), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	cycle(t.Context(), RunConfig{FileConfig: config, Version: "test", Control: control, Executor: Executor{Config: config}, Publisher: irePublisher, Spool: spool, Logger: discardLogger()}, discardLogger())
	result := waitResult(t, received)
	if !result.Success || result.Assets < 2 || result.Relationships < 1 || result.CollectionErrors != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func newServiceNowFixture(t *testing.T, job Job) (*Client, servicenow.Publisher, <-chan JobResult) {
	t.Helper()
	results := make(chan JobResult, 1)
	var mu sync.Mutex
	claimed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer relay-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case pollPath:
			mu.Lock()
			jobs := []Job(nil)
			if !claimed {
				claimed = true
				jobs = []Job{job}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(pollResponse{Jobs: jobs})
		case "/api/now/identifyreconcile/enhanced":
			body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
			if err != nil || len(body) == 0 {
				t.Errorf("IRE body length=%d err=%v", len(body), err)
			}
			_, _ = w.Write([]byte(`{"result":{"hasError":false}}`))
		case resultPath:
			var result JobResult
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Errorf("decode result: %v", err)
			}
			results <- result
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "relay-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	irePublisher := servicenow.Publisher{Config: servicenow.Config{InstanceURL: server.URL, Token: "relay-token", DiscoverySource: "Nischoy Topo", HTTPClient: server.Client()}}
	return client, irePublisher, results
}

func waitResult(t *testing.T, results <-chan JobResult) JobResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ServiceNow result")
		return JobResult{}
	}
}

type fakeControl struct {
	jobs           []Job
	results        []JobResult
	reportFailures int
	reportCalls    int
}

func (f *fakeControl) Poll(context.Context, PollRequest) ([]Job, error) {
	jobs := f.jobs
	f.jobs = nil
	return jobs, nil
}

func (f *fakeControl) Report(_ context.Context, result JobResult) error {
	f.reportCalls++
	if f.reportCalls <= f.reportFailures {
		return errors.New("temporary result failure")
	}
	f.results = append(f.results, result)
	return nil
}

type flakyPublisher struct {
	failures int
	calls    int
}

func (f *flakyPublisher) PublishBatch(context.Context, []model.ObservationEnvelope) (publisher.Result, error) {
	f.calls++
	if f.calls <= f.failures {
		return publisher.Result{}, retryableTestError{errors.New("temporary IRE failure")}
	}
	return publisher.Result{Destination: "servicenow-ire", Published: 1}, nil
}

type retryableTestError struct{ error }

func (retryableTestError) Retryable() bool { return true }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
