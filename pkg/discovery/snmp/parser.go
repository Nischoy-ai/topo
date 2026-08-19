package snmp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/gosnmp/gosnmp"
)

// MIB-II system group (RFC 1213), scalar OIDs (instance .0).
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
)

// MIB-II interfaces group ifTable columns (RFC 1213 / RFC 2863), indexed by ifIndex.
const (
	oidIfDescr    = "1.3.6.1.2.1.2.2.1.2"
	oidIfPhysAddr = "1.3.6.1.2.1.2.2.1.6"
)

// maxInterfaces bounds each ifTable column walk so a malformed or hostile
// agent cannot force unbounded memory use, matching the WinRM enumeration
// object cap.
const maxInterfaces = 4096

type Interface struct {
	Index     int
	Name, MAC string
}

type Inventory struct {
	EngineID       string
	SysName        string
	SysDescr       string
	SysObjectID    string
	SysUpTimeTicks uint32
	Interfaces     []Interface
}

func parseSystemGroup(variables []gosnmp.SnmpPDU) (sysDescr, sysObjectID, sysName string, sysUpTimeTicks uint32, err error) {
	for _, v := range variables {
		switch normalizeOID(v.Name) {
		case oidSysDescr:
			if sysDescr, err = octetString(v); err != nil {
				return "", "", "", 0, err
			}
		case oidSysObjectID:
			sysObjectID = objectIdentifier(v)
		case oidSysName:
			if sysName, err = octetString(v); err != nil {
				return "", "", "", 0, err
			}
		case oidSysUpTime:
			sysUpTimeTicks = timeTicks(v)
		}
	}
	return sysDescr, sysObjectID, sysName, sysUpTimeTicks, nil
}

func (p Plugin) walkInterfaces(client *gosnmp.GoSNMP) ([]Interface, error) {
	names := map[int]string{}
	macs := map[int]string{}
	seen := map[int]bool{}
	var order []int

	track := func(index int) {
		if !seen[index] {
			seen[index] = true
			order = append(order, index)
		}
	}

	descrCount := 0
	if err := client.BulkWalk(oidIfDescr, func(pdu gosnmp.SnmpPDU) error {
		descrCount++
		if descrCount > maxInterfaces {
			return fmt.Errorf("interface table exceeded %d entries", maxInterfaces)
		}
		index, err := lastOIDIndex(pdu.Name)
		if err != nil {
			return err
		}
		name, err := octetString(pdu)
		if err != nil {
			return err
		}
		names[index] = name
		track(index)
		return nil
	}); err != nil {
		return nil, err
	}

	macCount := 0
	if err := client.BulkWalk(oidIfPhysAddr, func(pdu gosnmp.SnmpPDU) error {
		macCount++
		if macCount > maxInterfaces {
			return fmt.Errorf("interface table exceeded %d entries", maxInterfaces)
		}
		index, err := lastOIDIndex(pdu.Name)
		if err != nil {
			return err
		}
		if raw, ok := pdu.Value.([]byte); ok && len(raw) > 0 {
			macs[index] = formatMAC(raw)
		}
		track(index)
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Ints(order)
	interfaces := make([]Interface, 0, len(order))
	for _, index := range order {
		interfaces = append(interfaces, Interface{Index: index, Name: names[index], MAC: macs[index]})
	}
	return interfaces, nil
}

func (i Inventory) Assets(now time.Time) ([]model.Asset, []model.Relationship) {
	ev := []model.Evidence{{Source: "snmp-network", Collected: now, Confidence: 1}}
	host := model.Asset{
		Type:     model.AssetHost,
		NativeID: i.EngineID,
		Name:     i.SysName,
		Identifiers: map[string]string{
			"engine_id": i.EngineID,
			"sys_name":  i.SysName,
		},
		Attributes: map[string]any{
			"sys_descr":         i.SysDescr,
			"sys_object_id":     i.SysObjectID,
			"sys_up_time_ticks": i.SysUpTimeTicks,
		},
		Evidence: ev,
	}
	assets := []model.Asset{host}
	rels := []model.Relationship{}
	for _, iface := range i.Interfaces {
		native := fmt.Sprintf("%s:interface:%d", i.EngineID, iface.Index)
		assets = append(assets, model.Asset{
			Type:     model.AssetNetworkInterface,
			NativeID: native,
			Name:     iface.Name,
			Identifiers: map[string]string{
				"host_engine_id": i.EngineID,
				"mac_address":    iface.MAC,
			},
			Attributes: map[string]any{
				"if_index":    iface.Index,
				"mac_address": iface.MAC,
			},
			Evidence: ev,
		})
		rels = append(rels, model.Relationship{Type: "host_has_interface", FromNativeID: i.EngineID, ToNativeID: native, Evidence: ev})
	}
	return assets, rels
}

func octetString(pdu gosnmp.SnmpPDU) (string, error) {
	switch pdu.Type {
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		return "", fmt.Errorf("OID %s not available on device", pdu.Name)
	}
	raw, ok := pdu.Value.([]byte)
	if !ok {
		return "", fmt.Errorf("OID %s: expected an octet string, got %s", pdu.Name, pdu.Type)
	}
	return string(raw), nil
}

func objectIdentifier(pdu gosnmp.SnmpPDU) string {
	if s, ok := pdu.Value.(string); ok {
		return strings.TrimPrefix(s, ".")
	}
	return ""
}

func timeTicks(pdu gosnmp.SnmpPDU) uint32 {
	if v, ok := pdu.Value.(uint32); ok {
		return v
	}
	return 0
}

func lastOIDIndex(name string) (int, error) {
	normalized := normalizeOID(name)
	parts := strings.Split(normalized, ".")
	last := parts[len(parts)-1]
	index, err := strconv.Atoi(last)
	if err != nil {
		return 0, fmt.Errorf("OID %q has a non-numeric index", name)
	}
	return index, nil
}

func normalizeOID(name string) string {
	return strings.TrimPrefix(name, ".")
}

func formatMAC(raw []byte) string {
	parts := make([]string, len(raw))
	for i, b := range raw {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(parts, ":")
}
