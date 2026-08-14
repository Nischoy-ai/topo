package sshlinux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

type Inventory struct {
	Hostname, MachineID, OSName, OSVersion, Architecture, Serial string
	CPUCount, MemoryMB                                           int
	Interfaces                                                   []Interface
	Packages, Services                                           []string
}

type Interface struct {
	Index     int
	Name, MAC string
	Addresses []string
}

type ipAddress struct {
	IfIndex  int    `json:"ifindex"`
	IfName   string `json:"ifname"`
	Address  string `json:"address"`
	AddrInfo []struct {
		Local     string `json:"local"`
		PrefixLen int    `json:"prefixlen"`
	} `json:"addr_info"`
}

func ParseOSRelease(raw string) (string, string) {
	values := map[string]string{}
	s := bufio.NewScanner(strings.NewReader(raw))
	for s.Scan() {
		parts := strings.SplitN(s.Text(), "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.Trim(parts[1], `"`)
		}
	}
	name := values["NAME"]
	if name == "" {
		name = values["ID"]
	}
	return name, values["VERSION_ID"]
}

func ParseInterfaces(raw string) ([]Interface, error) {
	var entries []ipAddress
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse ip address JSON: %w", err)
	}
	out := make([]Interface, 0, len(entries))
	for _, e := range entries {
		addresses := make([]string, 0, len(e.AddrInfo))
		for _, a := range e.AddrInfo {
			addresses = append(addresses, fmt.Sprintf("%s/%d", a.Local, a.PrefixLen))
		}
		out = append(out, Interface{Index: e.IfIndex, Name: e.IfName, MAC: e.Address, Addresses: addresses})
	}
	return out, nil
}

func ParsePositiveInt(field, raw string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 1 {
		return 0, fmt.Errorf("invalid %s %q", field, strings.TrimSpace(raw))
	}
	return v, nil
}

func ParseLines(raw string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	s := bufio.NewScanner(strings.NewReader(raw))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			line = fields[0]
		}
		if _, ok := seen[line]; !ok {
			seen[line] = struct{}{}
			out = append(out, line)
		}
	}
	return out
}

func ParseServices(raw string) []string {
	services := ParseLines(raw)
	for index, service := range services {
		services[index] = strings.TrimSuffix(service, ".service")
	}
	return services
}

func (i Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "ssh-linux", Collected: now, Confidence: 1}}
	attrs := map[string]any{"os": "linux", "os_name": i.OSName, "os_version": i.OSVersion, "architecture": i.Architecture, "cpu_logical_count": i.CPUCount, "memory_mb": i.MemoryMB, "packages": i.Packages, "services": i.Services}
	host := model.Asset{Type: model.AssetHost, NativeID: i.MachineID, Name: i.Hostname, Identifiers: map[string]string{"machine_id": i.MachineID, "serial_number": i.Serial, "hostname": i.Hostname}, Attributes: attrs, Evidence: ev}
	assets := []model.Asset{host}
	rels := []model.Relationship{}
	for _, iface := range i.Interfaces {
		native := fmt.Sprintf("%s:interface:%d", i.MachineID, iface.Index)
		assets = append(assets, model.Asset{Type: model.AssetNetworkInterface, NativeID: native, Name: iface.Name, Identifiers: map[string]string{"host_machine_id": i.MachineID, "mac_address": iface.MAC}, Attributes: map[string]any{"mac_address": iface.MAC, "addresses": iface.Addresses}, Evidence: ev})
		rels = append(rels, model.Relationship{Type: "host_has_interface", FromNativeID: i.MachineID, ToNativeID: native, Evidence: ev})
	}
	return assets, rels
}
