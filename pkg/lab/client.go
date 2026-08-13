package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type RunOptions struct {
	BaseURL        string
	Concurrency    int
	RequestTimeout time.Duration
	HTTPClient     *http.Client
}
type RunResult struct {
	Observation model.ObservationEnvelope `json:"observation"`
	Attempted   int                       `json:"attempted"`
	Discovered  int                       `json:"discovered"`
	Partial     int                       `json:"partial"`
	Failed      int                       `json:"failed"`
	Duration    time.Duration             `json:"duration"`
}
type catalog struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
}

func Discover(ctx context.Context, o RunOptions) (RunResult, error) {
	start := time.Now()
	if o.Concurrency < 1 {
		o.Concurrency = 32
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = 2 * time.Second
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: o.RequestTimeout}
	}
	var c catalog
	if err := getJSON(ctx, client, strings.TrimRight(o.BaseURL, "/")+"/v1/catalog", &c, nil); err != nil {
		return RunResult{}, fmt.Errorf("catalog: %w", err)
	}
	result := RunResult{Attempted: len(c.Items)}
	now := time.Now().UTC()
	obs := model.ObservationEnvelope{SchemaVersion: model.SchemaVersion, ObservationID: randomObservationID(), SiteID: "lab", CollectorID: "topo-lab-client", Plugin: "topo-lab", ObservedAt: now}
	jobs := make(chan string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range o.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				reqCtx, cancel := context.WithTimeout(ctx, o.RequestTimeout)
				var h Host
				headers := http.Header{}
				err := getJSON(reqCtx, client, strings.TrimRight(o.BaseURL, "/")+"/v1/hosts/"+id+"/inventory", &h, headers)
				cancel()
				mu.Lock()
				if err != nil {
					result.Failed++
					obs.Errors = append(obs.Errors, model.CollectionError{Code: "lab_endpoint_error", Message: id + ": " + err.Error(), Retryable: true})
				} else {
					partial := headers.Get("X-Topo-Lab-Partial") != ""
					if partial {
						result.Partial++
					}
					result.Discovered++
					assets, rels := assetsForHost(h, now, partial)
					obs.Assets = append(obs.Assets, assets...)
					obs.Relationships = append(obs.Relationships, rels...)
				}
				mu.Unlock()
			}
		}()
	}
	for _, item := range c.Items {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return RunResult{}, ctx.Err()
		case jobs <- item.ID:
		}
	}
	close(jobs)
	wg.Wait()
	result.Observation = obs
	result.Duration = time.Since(start)
	return result, nil
}
func getJSON(ctx context.Context, c *http.Client, url string, out any, headers http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if headers != nil {
		for k, v := range resp.Header {
			headers[k] = append([]string(nil), v...)
		}
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
func assetsForHost(h Host, now time.Time, partial bool) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "topo-lab-client", Collected: now, Confidence: 1}}
	attrs := map[string]any{"os": h.OS, "os_version": h.OSVersion, "architecture": h.Architecture, "cpu_logical_count": h.CPUCount, "memory_mb": h.MemoryMB, "ip_address": h.IPAddress}
	if !partial {
		attrs["packages"] = h.Packages
		attrs["services"] = h.Services
	}
	host := model.Asset{Type: model.AssetHost, NativeID: h.MachineID, Name: h.Name, Identifiers: map[string]string{"machine_id": h.MachineID, "serial_number": h.Serial, "hostname": h.Name}, Attributes: attrs, Evidence: ev}
	ifaceID := h.MachineID + ":interface:1"
	iface := model.Asset{Type: model.AssetNetworkInterface, NativeID: ifaceID, Name: "primary", Identifiers: map[string]string{"host_machine_id": h.MachineID, "mac_address": h.MACAddress}, Attributes: map[string]any{"addresses": []string{h.IPAddress}, "mac_address": h.MACAddress}, Evidence: ev}
	rel := model.Relationship{Type: "host_has_interface", FromNativeID: h.MachineID, ToNativeID: ifaceID, Evidence: ev}
	return []model.Asset{host, iface}, []model.Relationship{rel}
}
func randomObservationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
