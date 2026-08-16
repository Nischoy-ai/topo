package sshlinux_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"github.com/Nischoy-ai/topo/pkg/discovery/winrm"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestDiscoverMixedFiveHundredLinuxAndWindowsHostsTwice(t *testing.T) {
	estate := makeEstate(t, lab.DefaultScenario(1000, 50, 84))
	sshServer := makeServer(t, estate)
	sshPlugin := pluginForServer(sshServer)
	winrmPlugin := winrm.Plugin{Config: winrm.Config{
		Username:         lab.LabWinRMUsername,
		Password:         lab.LabWinRMPassword,
		LabMode:          true,
		Concurrency:      64,
		OperationTimeout: 10 * time.Second,
		HTTPClient:       &http.Client{Transport: mixedHandlerTransport{handler: lab.NewWinRMServer(estate).Handler()}},
	}}
	sshTargets, winrmTargets := mixedTargets(estate)
	if len(sshTargets) != 500 || len(winrmTargets) != 500 {
		t.Fatalf("got Linux targets=%d Windows targets=%d, want 500 each", len(sshTargets), len(winrmTargets))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	repository := store.NewMemory()
	for scan := 1; scan <= 2; scan++ {
		sshObservation, winrmObservation := discoverMixed(t, ctx, sshPlugin, winrmPlugin, sshTargets, winrmTargets, scan)
		combined := combineObservations(sshObservation, winrmObservation)
		assertCompleteMixedEstate(t, estate, combined)
		assertMixedInventory(t, combined)
		if err := repository.SaveObservation(ctx, sshObservation); err != nil {
			t.Fatal(err)
		}
		if err := repository.SaveObservation(ctx, winrmObservation); err != nil {
			t.Fatal(err)
		}
	}

	assets, err := repository.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2000 {
		t.Fatalf("repeated mixed scan created duplicates: got %d resolved assets, want 2000", len(assets))
	}
	observations, err := repository.ListObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 4 {
		t.Fatalf("stored %d source observations, want two plugins across two scans", len(observations))
	}
}

type mixedResult struct {
	observation model.ObservationEnvelope
	err         error
}

func discoverMixed(t *testing.T, ctx context.Context, sshPlugin sshlinux.Plugin, winrmPlugin winrm.Plugin, sshTargets, winrmTargets []string, scan int) (model.ObservationEnvelope, model.ObservationEnvelope) {
	t.Helper()
	sshResult := make(chan mixedResult, 1)
	winrmResult := make(chan mixedResult, 1)
	go func() {
		observation, err := sshPlugin.Discover(ctx, discovery.Request{SiteID: "lab", CollectorID: "mixed-relay", JobID: fmt.Sprintf("mixed-%d-linux", scan), Targets: sshTargets})
		sshResult <- mixedResult{observation: observation, err: err}
	}()
	go func() {
		observation, err := winrmPlugin.Discover(ctx, discovery.Request{SiteID: "lab", CollectorID: "mixed-relay", JobID: fmt.Sprintf("mixed-%d-windows", scan), Targets: winrmTargets})
		winrmResult <- mixedResult{observation: observation, err: err}
	}()
	ssh := <-sshResult
	windows := <-winrmResult
	if ssh.err != nil {
		t.Fatalf("mixed scan %d SSH discovery: %v", scan, ssh.err)
	}
	if windows.err != nil {
		t.Fatalf("mixed scan %d WinRM discovery: %v", scan, windows.err)
	}
	return ssh.observation, windows.observation
}

func mixedTargets(estate lab.Estate) ([]string, []string) {
	sshTargets := []string{}
	winrmTargets := []string{}
	for _, host := range estate.Hosts {
		switch host.OS {
		case "linux":
			sshTargets = append(sshTargets, host.ID+"@lab.test:22")
		case "windows":
			winrmTargets = append(winrmTargets, "http://127.0.0.1/wsman/"+host.ID)
		}
	}
	return sshTargets, winrmTargets
}

func combineObservations(observations ...model.ObservationEnvelope) model.ObservationEnvelope {
	combined := model.ObservationEnvelope{SchemaVersion: model.SchemaVersion, SiteID: "lab", CollectorID: "mixed-relay", Plugin: "mixed-acceptance"}
	for _, observation := range observations {
		combined.Assets = append(combined.Assets, observation.Assets...)
		combined.Relationships = append(combined.Relationships, observation.Relationships...)
		combined.Errors = append(combined.Errors, observation.Errors...)
	}
	return combined
}

func assertCompleteMixedEstate(t *testing.T, estate lab.Estate, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("mixed discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
	evaluation := lab.Evaluate(estate.Expected, observation)
	if evaluation.ExpectedAssets != 2000 || evaluation.ActualAssets != 2000 || evaluation.CoveragePercent != 100 || evaluation.MissingAssets != 0 || evaluation.UnexpectedAssets != 0 {
		t.Fatalf("unexpected mixed evaluation: %#v", evaluation)
	}
	if len(observation.Relationships) != 1000 {
		t.Fatalf("got %d mixed relationships, want 1000", len(observation.Relationships))
	}
}

func assertMixedInventory(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	linuxHosts := 0
	windowsHosts := 0
	for _, asset := range observation.Assets {
		if asset.Type != model.AssetHost {
			continue
		}
		switch asset.Attributes["os"] {
		case "linux":
			linuxHosts++
			packages, ok := asset.Attributes["packages"].([]string)
			if !ok || len(packages) == 0 {
				t.Fatalf("Linux host %s omitted package inventory", asset.Name)
			}
		case "windows":
			windowsHosts++
			software, ok := asset.Attributes["software"].([]winrm.Software)
			if !ok || len(software) == 0 {
				t.Fatalf("Windows host %s omitted software inventory", asset.Name)
			}
		default:
			t.Fatalf("host %s has unexpected OS attribute %#v", asset.Name, asset.Attributes["os"])
		}
	}
	if linuxHosts != 500 || windowsHosts != 500 {
		t.Fatalf("got Linux hosts=%d Windows hosts=%d, want 500 each", linuxHosts, windowsHosts)
	}
}

type mixedHandlerTransport struct{ handler http.Handler }

func (transport mixedHandlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	if response.Body == nil {
		response.Body = io.NopCloser(strings.NewReader(""))
	}
	response.Request = request
	return response, nil
}
