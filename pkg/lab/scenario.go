// Package lab provides deterministic simulated IT estates for Topo testing.
package lab

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

const ScenarioVersion = "v1alpha1"

type Scenario struct {
	Version string      `json:"version" yaml:"version"`
	Seed    int64       `json:"seed" yaml:"seed"`
	SiteID  string      `json:"site_id" yaml:"site_id"`
	Hosts   []HostGroup `json:"hosts" yaml:"hosts"`
	Faults  Faults      `json:"faults,omitempty" yaml:"faults,omitempty"`
}

type HostGroup struct {
	Persona string `json:"persona" yaml:"persona"`
	Count   int    `json:"count" yaml:"count"`
}

type Faults struct {
	AuthFailurePercent      float64 `json:"auth_failure_percent,omitempty" yaml:"auth_failure_percent,omitempty"`
	TimeoutPercent          float64 `json:"timeout_percent,omitempty" yaml:"timeout_percent,omitempty"`
	PermissionDeniedPercent float64 `json:"permission_denied_percent,omitempty" yaml:"permission_denied_percent,omitempty"`
	MalformedPercent        float64 `json:"malformed_percent,omitempty" yaml:"malformed_percent,omitempty"`
	DisappearPercent        float64 `json:"disappear_after_first_scan_percent,omitempty" yaml:"disappear_after_first_scan_percent,omitempty"`
	LatencyMS               int     `json:"latency_ms,omitempty" yaml:"latency_ms,omitempty"`
	JitterMS                int     `json:"jitter_ms,omitempty" yaml:"jitter_ms,omitempty"`
}

type Host struct {
	ID           string   `json:"id"`
	Persona      string   `json:"persona"`
	Name         string   `json:"name"`
	MachineID    string   `json:"machine_id"`
	Serial       string   `json:"serial"`
	IPAddress    string   `json:"ip_address"`
	MACAddress   string   `json:"mac_address"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os_version"`
	Architecture string   `json:"architecture"`
	CPUCount     int      `json:"cpu_count"`
	MemoryMB     int      `json:"memory_mb"`
	Packages     []string `json:"packages"`
	Services     []string `json:"services"`
	Fault        string   `json:"fault,omitempty"`
	LatencyMS    int      `json:"latency_ms,omitempty"`
}

type Estate struct {
	Scenario Scenario                  `json:"scenario"`
	Hosts    []Host                    `json:"hosts"`
	Expected model.ObservationEnvelope `json:"expected_observation"`
}

var personas = map[string]struct {
	OS, Version, Arch  string
	Packages, Services []string
}{
	"ubuntu-22.04": {"linux", "Ubuntu 22.04", "x86_64", []string{"bash", "coreutils", "openssh-server", "nginx"}, []string{"sshd", "nginx"}},
	"ubuntu-24.04": {"linux", "Ubuntu 24.04", "x86_64", []string{"bash", "coreutils", "openssh-server", "podman"}, []string{"sshd", "podman"}},
	"rocky-9":      {"linux", "Rocky Linux 9", "x86_64", []string{"bash", "coreutils", "openssh-server", "postgresql"}, []string{"sshd", "postgresql"}},
	"windows-2019": {"windows", "Windows Server 2019", "x86_64", []string{"PowerShell", "OpenSSH.Client"}, []string{"WinRM", "W32Time"}},
	"windows-2022": {"windows", "Windows Server 2022", "x86_64", []string{"PowerShell", "Microsoft Defender"}, []string{"WinRM", "W32Time", "WinDefend"}},
	"windows-2025": {"windows", "Windows Server 2025", "x86_64", []string{"PowerShell", "Microsoft Defender"}, []string{"WinRM", "W32Time", "WinDefend"}},
}

func DefaultScenario(count, windowsPercent int, seed int64) Scenario {
	if count < 1 {
		count = 1
	}
	if windowsPercent < 0 {
		windowsPercent = 0
	}
	if windowsPercent > 100 {
		windowsPercent = 100
	}
	windows := count * windowsPercent / 100
	linux := count - windows
	hosts := []HostGroup{}
	if linux > 0 {
		hosts = append(hosts, HostGroup{Persona: "ubuntu-24.04", Count: linux})
	}
	if windows > 0 {
		hosts = append(hosts, HostGroup{Persona: "windows-2022", Count: windows})
	}
	return Scenario{Version: ScenarioVersion, Seed: seed, SiteID: "lab", Hosts: hosts}
}

func (s Scenario) Validate() error {
	if s.Version != ScenarioVersion {
		return fmt.Errorf("unsupported scenario version %q", s.Version)
	}
	if s.SiteID == "" {
		return errors.New("site_id is required")
	}
	total := 0
	for _, g := range s.Hosts {
		if _, ok := personas[g.Persona]; !ok {
			return fmt.Errorf("unknown persona %q", g.Persona)
		}
		if g.Count < 0 {
			return errors.New("host count cannot be negative")
		}
		total += g.Count
	}
	if total == 0 {
		return errors.New("scenario must contain at least one host")
	}
	if total > 100000 {
		return errors.New("scenario exceeds 100000 host safety limit")
	}
	for name, v := range map[string]float64{"auth_failure": s.Faults.AuthFailurePercent, "timeout": s.Faults.TimeoutPercent, "permission_denied": s.Faults.PermissionDeniedPercent, "malformed": s.Faults.MalformedPercent, "disappear": s.Faults.DisappearPercent} {
		if v < 0 || v > 100 {
			return fmt.Errorf("%s percentage must be between 0 and 100", name)
		}
	}
	totalFaults := s.Faults.AuthFailurePercent + s.Faults.TimeoutPercent + s.Faults.PermissionDeniedPercent + s.Faults.MalformedPercent + s.Faults.DisappearPercent
	if totalFaults > 100 {
		return errors.New("fault percentages cannot total more than 100")
	}
	if s.Faults.LatencyMS < 0 || s.Faults.JitterMS < 0 {
		return errors.New("latency values cannot be negative")
	}
	return nil
}

func Generate(s Scenario) (Estate, error) {
	if err := s.Validate(); err != nil {
		return Estate{}, err
	}
	rng := rand.New(rand.NewSource(s.Seed))
	hosts := []Host{}
	idx := 0
	for _, g := range s.Hosts {
		p := personas[g.Persona]
		for range g.Count {
			idx++
			id := fmt.Sprintf("host-%06d", idx)
			mac := fmt.Sprintf("02:54:%02x:%02x:%02x:%02x", (idx>>24)&255, (idx>>16)&255, (idx>>8)&255, idx&255)
			h := Host{ID: id, Persona: g.Persona, Name: fmt.Sprintf("%s-%05d", shortOS(p.OS), idx), MachineID: stableToken(s.Seed, id, "machine"), Serial: "TOPOLAB-" + stableToken(s.Seed, id, "serial")[:12], IPAddress: net.IPv4(10, byte(1+(idx/65025)%200), byte((idx/255)%255), byte(1+idx%254)).String(), MACAddress: mac, OS: p.OS, OSVersion: p.Version, Architecture: p.Arch, CPUCount: []int{2, 4, 8}[rng.Intn(3)], MemoryMB: []int{4096, 8192, 16384}[rng.Intn(3)], Packages: append([]string(nil), p.Packages...), Services: append([]string(nil), p.Services...), LatencyMS: s.Faults.LatencyMS}
			if s.Faults.JitterMS > 0 {
				h.LatencyMS += rng.Intn(s.Faults.JitterMS + 1)
			}
			h.Fault = selectFault(rng, s.Faults)
			hosts = append(hosts, h)
		}
	}
	return Estate{Scenario: s, Hosts: hosts, Expected: expectedObservation(s, hosts)}, nil
}

func expectedObservation(s Scenario, hosts []Host) model.ObservationEnvelope {
	now := time.Unix(0, 0).UTC()
	assets := make([]model.Asset, 0, len(hosts)*2)
	rels := make([]model.Relationship, 0, len(hosts))
	for _, h := range hosts {
		ev := []model.Evidence{{Source: "topo-lab", Collected: now, Confidence: 1}}
		attrs := map[string]any{"os": h.OS, "os_version": h.OSVersion, "architecture": h.Architecture, "cpu_logical_count": h.CPUCount, "memory_mb": h.MemoryMB, "packages": h.Packages, "services": h.Services, "ip_address": h.IPAddress}
		assets = append(assets, model.Asset{Type: model.AssetHost, NativeID: h.MachineID, Name: h.Name, Identifiers: map[string]string{"machine_id": h.MachineID, "serial_number": h.Serial, "hostname": h.Name}, Attributes: attrs, Evidence: ev})
		ifaceID := h.MachineID + ":interface:1"
		assets = append(assets, model.Asset{Type: model.AssetNetworkInterface, NativeID: ifaceID, Name: "primary", Identifiers: map[string]string{"host_machine_id": h.MachineID, "mac_address": h.MACAddress}, Attributes: map[string]any{"addresses": []string{h.IPAddress}, "mac_address": h.MACAddress}, Evidence: ev})
		rels = append(rels, model.Relationship{Type: "host_has_interface", FromNativeID: h.MachineID, ToNativeID: ifaceID, Evidence: ev})
	}
	return model.ObservationEnvelope{SchemaVersion: model.SchemaVersion, ObservationID: "expected-" + stableToken(s.Seed, s.SiteID, "observation"), SiteID: s.SiteID, CollectorID: "topo-lab", Plugin: "topo-lab", ObservedAt: now, Assets: assets, Relationships: rels}
}

func selectFault(r *rand.Rand, f Faults) string {
	x := r.Float64() * 100
	for _, v := range []struct {
		n string
		p float64
	}{{"auth_failure", f.AuthFailurePercent}, {"timeout", f.TimeoutPercent}, {"permission_denied", f.PermissionDeniedPercent}, {"malformed", f.MalformedPercent}, {"disappear", f.DisappearPercent}} {
		if x < v.p {
			return v.n
		}
		x -= v.p
	}
	return ""
}
func stableToken(seed int64, parts ...string) string {
	h := sha256.New()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h.Write(b[:])
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
func shortOS(os string) string {
	if os == "windows" {
		return "win"
	}
	return "linux"
}
