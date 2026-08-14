package winrm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/winrm"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestDiscoverFiveHundredWindowsHostsThroughWSManagement(t *testing.T) {
	estate := makeWindowsEstate(t, lab.DefaultScenario(500, 100, 42))
	plugin := pluginForEstate(estate)
	targets := targetsForEstate(estate)
	request := discovery.Request{SiteID: estate.Scenario.SiteID, CollectorID: "winrm-test", Targets: targets}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCompleteWindowsEstate(t, estate, first)
	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCompleteWindowsEstate(t, estate, second)
	if err := repository.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}
	assets, err := repository.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1000 {
		t.Fatalf("repeated WinRM scan created duplicates: got %d assets, want 1000", len(assets))
	}
}

func TestWinRMPermissionFailureReturnsPartialHost(t *testing.T) {
	scenario := lab.DefaultScenario(1, 100, 7)
	scenario.Faults.PermissionDeniedPercent = 100
	observation := discoverOne(t, makeWindowsEstate(t, scenario), lab.LabWinRMPassword, 2*time.Second, 4<<20)
	if len(observation.Assets) != 1 || len(observation.Errors) != 1 {
		t.Fatalf("got assets=%d errors=%d, want partial host and one error", len(observation.Assets), len(observation.Errors))
	}
	if observation.Errors[0].Code != "winrm_partial" || observation.Errors[0].Retryable {
		t.Fatalf("unexpected partial error: %#v", observation.Errors[0])
	}
}

func TestWinRMMalformedAuthenticationTimeoutAndBoundsAreIsolated(t *testing.T) {
	for _, test := range []struct {
		name         string
		fault        lab.Faults
		password     string
		timeout      time.Duration
		maximumBytes int64
	}{
		{name: "malformed", fault: lab.Faults{MalformedPercent: 100}, password: lab.LabWinRMPassword, timeout: time.Second, maximumBytes: 4 << 20},
		{name: "authentication", password: "wrong", timeout: time.Second, maximumBytes: 4 << 20},
		{name: "timeout", fault: lab.Faults{TimeoutPercent: 100}, password: lab.LabWinRMPassword, timeout: 10 * time.Millisecond, maximumBytes: 4 << 20},
		{name: "bounded response", password: lab.LabWinRMPassword, timeout: time.Second, maximumBytes: 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			scenario := lab.DefaultScenario(1, 100, 8)
			scenario.Faults = test.fault
			observation := discoverOne(t, makeWindowsEstate(t, scenario), test.password, test.timeout, test.maximumBytes)
			if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "winrm_operation" {
				t.Fatalf("unexpected observation: %#v", observation)
			}
		})
	}
}

func TestLabWinRMRejectsArbitrarySOAPOperations(t *testing.T) {
	estate := makeWindowsEstate(t, lab.DefaultScenario(1, 100, 9))
	handler := lab.NewWinRMServer(estate).Handler()
	resource := winrm.AuditedOperations()[0].ResourceURI
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:w="http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd" xmlns:r="http://schemas.microsoft.com/wbem/wsman/1/windows/shell"><s:Header><a:Action>` + winrm.ActionEnumerate + `</a:Action><w:ResourceURI>` + resource + `</w:ResourceURI></s:Header><s:Body><r:Command>whoami</r:Command></s:Body></s:Envelope>`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/wsman/"+estate.Hosts[0].ID, strings.NewReader(body))
	request.SetBasicAuth(lab.LabWinRMUsername, lab.LabWinRMPassword)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid audited") {
		t.Fatalf("arbitrary operation was not rejected: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWinRMConnectivityAndCancellation(t *testing.T) {
	estate := makeWindowsEstate(t, lab.DefaultScenario(1, 100, 10))
	plugin := pluginForEstate(estate)
	request := discovery.Request{Targets: targetsForEstate(estate)}
	if err := plugin.CheckConnectivity(context.Background(), request); err != nil {
		t.Fatalf("connectivity check failed: %v", err)
	}
	plugin.Config.Password = "wrong"
	if err := plugin.CheckConnectivity(context.Background(), request); err == nil {
		t.Fatal("connectivity check accepted invalid authentication")
	}

	scenario := lab.DefaultScenario(1, 100, 11)
	scenario.Faults.TimeoutPercent = 100
	timeoutEstate := makeWindowsEstate(t, scenario)
	timeoutPlugin := pluginForEstate(timeoutEstate)
	timeoutPlugin.Config.OperationTimeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := timeoutPlugin.Discover(ctx, discovery.Request{Targets: targetsForEstate(timeoutEstate)})
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Discover returned %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Discover did not stop after cancellation")
	}
}

func pluginForEstate(estate lab.Estate) winrm.Plugin {
	return winrm.Plugin{Config: winrm.Config{
		Username:         lab.LabWinRMUsername,
		Password:         lab.LabWinRMPassword,
		LabMode:          true,
		Concurrency:      64,
		OperationTimeout: 2 * time.Second,
		HTTPClient:       &http.Client{Transport: handlerTransport{handler: lab.NewWinRMServer(estate).Handler()}},
	}}
}

func discoverOne(t *testing.T, estate lab.Estate, password string, timeout time.Duration, maximumBytes int64) model.ObservationEnvelope {
	t.Helper()
	plugin := pluginForEstate(estate)
	plugin.Config.Password = password
	plugin.Config.OperationTimeout = timeout
	plugin.Config.MaxResponseBytes = maximumBytes
	observation, err := plugin.Discover(context.Background(), discovery.Request{SiteID: "lab", Targets: targetsForEstate(estate)})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func makeWindowsEstate(t *testing.T, scenario lab.Scenario) lab.Estate {
	t.Helper()
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	return estate
}

func targetsForEstate(estate lab.Estate) []string {
	targets := make([]string, 0, len(estate.Hosts))
	for _, host := range estate.Hosts {
		if host.OS == "windows" {
			targets = append(targets, "http://127.0.0.1/wsman/"+host.ID)
		}
	}
	return targets
}

func assertCompleteWindowsEstate(t *testing.T, estate lab.Estate, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("WinRM discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
	evaluation := lab.Evaluate(estate.Expected, observation)
	if evaluation.CoveragePercent != 100 || evaluation.MissingAssets != 0 || evaluation.UnexpectedAssets != 0 {
		t.Fatalf("unexpected evaluation: %#v", evaluation)
	}
}

type handlerTransport struct{ handler http.Handler }

func (transport handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	if response.Body == nil {
		response.Body = io.NopCloser(strings.NewReader(""))
	}
	response.Request = request
	return response, nil
}
