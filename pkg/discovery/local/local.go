// Package local discovers the machine on which Nischoy Topo is running without elevated privileges.
package local

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
)

type Plugin struct{}

func (Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{Name: "local-host", Version: "0.1.0", AssetTypes: []model.AssetType{model.AssetHost, model.AssetNetworkInterface}, RequiredPermissions: []string{"read host metadata", "list network interfaces"}}
}

func (Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) > 0 && (len(r.Targets) != 1 || r.Targets[0] != "local") {
		return discovery.ErrUnsupportedTarget
	}
	return nil
}

func (p Plugin) CheckConnectivity(ctx context.Context, r discovery.Request) error {
	return p.ValidateConfiguration(ctx, r)
}

func (p Plugin) Discover(ctx context.Context, r discovery.Request) (model.ObservationEnvelope, error) {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return model.ObservationEnvelope{}, err
	}
	now := time.Now().UTC()
	hostname, err := os.Hostname()
	if err != nil {
		return model.ObservationEnvelope{}, fmt.Errorf("hostname: %w", err)
	}
	machineID := readFirst("/etc/machine-id", "/var/lib/dbus/machine-id")
	if machineID == "" {
		machineID = hostname
	}
	attrs := map[string]any{"os": runtime.GOOS, "architecture": runtime.GOARCH, "cpu_logical_count": runtime.NumCPU()}
	for k, v := range osRelease() {
		attrs["os_"+k] = v
	}
	evidence := []model.Evidence{{Source: "local-host", Collected: now, Confidence: 1}}
	host := model.Asset{Type: model.AssetHost, NativeID: machineID, Name: hostname, Identifiers: map[string]string{"hostname": hostname, "machine_id": machineID}, Attributes: attrs, Evidence: evidence}
	assets := []model.Asset{host}
	rels := []model.Relationship{}
	interfaces, ifErr := net.Interfaces()
	if ifErr == nil {
		for _, iface := range interfaces {
			select {
			case <-ctx.Done():
				return model.ObservationEnvelope{}, ctx.Err()
			default:
			}
			addrs, _ := iface.Addrs()
			ips := make([]string, 0, len(addrs))
			for _, addr := range addrs {
				ips = append(ips, addr.String())
			}
			native := machineID + ":interface:" + strconv.Itoa(iface.Index)
			a := model.Asset{Type: model.AssetNetworkInterface, NativeID: native, Name: iface.Name, Identifiers: map[string]string{"host_machine_id": machineID, "interface_index": strconv.Itoa(iface.Index)}, Attributes: map[string]any{"mac_address": iface.HardwareAddr.String(), "addresses": ips, "mtu": iface.MTU, "flags": iface.Flags.String()}, Evidence: evidence}
			assets = append(assets, a)
			rels = append(rels, model.Relationship{Type: "host_has_interface", FromNativeID: machineID, ToNativeID: native, Evidence: evidence})
		}
	}
	return model.ObservationEnvelope{SchemaVersion: model.SchemaVersion, ObservationID: randomID(), SiteID: defaultString(r.SiteID, "default"), CollectorID: defaultString(r.CollectorID, hostname), Plugin: "local-host", JobID: r.JobID, ObservedAt: now, Assets: assets, Relationships: rels}, nil
}

func readFirst(paths ...string) string {
	for _, path := range paths {
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func osRelease() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		p := strings.SplitN(s.Text(), "=", 2)
		if len(p) == 2 {
			out[strings.ToLower(p[0])] = strings.Trim(p[1], `"`)
		}
	}
	return out
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
func defaultString(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
