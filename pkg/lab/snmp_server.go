package lab

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// LabSNMPUsername is the conventional SNMPv3 username Topo Lab clients use.
// It is not enforced by the server: noAuthNoPriv performs no authentication,
// so any username is accepted, exactly as a real noAuthNoPriv agent would.
const LabSNMPUsername = "topo-lab"

// MIB-II OIDs served by the Topo Lab SNMP agent — kept in sync with
// pkg/discovery/snmp's own constants deliberately, since this file has no
// import relationship with that package (a protocol test double should not
// depend on the plugin under test).
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidIfDescr     = "1.3.6.1.2.1.2.2.1.2"
	oidIfPhysAddr  = "1.3.6.1.2.1.2.2.1.6"
)

// SNMPServer exposes a Topo Lab estate over real SNMPv3 noAuthNoPriv
// messages, one loopback UDP socket per simulated host — unlike the
// SSH/WinRM Topo Lab servers, which multiplex one listener by username,
// SNMP has no connection-level handshake to carry an identity, so a
// distinct address per simulated device is both simpler and closer to how
// real network equipment is deployed (one IP per device). It answers
// noAuthNoPriv only: gosnmp is a client-only library with no equivalent of
// govmomi's vcsim to reuse, and implementing USM's RFC 3414/3826
// authentication/privacy crypto from scratch on the server side is out of
// scope for a test fixture. Message encoding and decoding reuse gosnmp's
// own exported SnmpDecodePacket and SnmpPacket.MarshalMsg, so the wire
// format exercised is real, not a simplified stand-in — this is what lets
// the two-scan idempotency acceptance test exercise the plugin's actual
// SNMPv3 message framing and engine ID discovery handshake.
type SNMPServer struct {
	Estate    Estate
	conns     []*net.UDPConn
	addresses map[string]string

	mu    sync.Mutex
	calls map[string]int
}

// NewSNMPServer binds one loopback UDP socket per host in estate and starts
// serving each in the background immediately; there is no separate Serve
// call. Close stops every socket.
func NewSNMPServer(estate Estate) (*SNMPServer, error) {
	server := &SNMPServer{Estate: estate, addresses: map[string]string{}, calls: map[string]int{}}
	for _, host := range estate.Hosts {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			_ = server.Close()
			return nil, fmt.Errorf("listen UDP for host %s: %w", host.ID, err)
		}
		server.conns = append(server.conns, conn)
		server.addresses[host.ID] = conn.LocalAddr().String()
		go server.serveHost(conn, host)
	}
	return server, nil
}

// Address returns the "host:port" a given simulated host's SNMP agent is
// reachable on.
func (s *SNMPServer) Address(hostID string) (string, bool) {
	addr, ok := s.addresses[hostID]
	return addr, ok
}

func (s *SNMPServer) Close() error {
	var err error
	for _, conn := range s.conns {
		if closeErr := conn.Close(); closeErr != nil {
			err = closeErr
		}
	}
	return err
}

func (s *SNMPServer) serveHost(conn *net.UDPConn, host Host) {
	buf := make([]byte, 65535)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		data := append([]byte(nil), buf[:n]...)
		go s.handle(conn, addr, data, host)
	}
}

func (s *SNMPServer) handle(conn *net.UDPConn, addr *net.UDPAddr, data []byte, host Host) {
	if host.Fault == "auth_failure" {
		return
	}
	request, err := decodeSNMPPacket(data)
	if err != nil {
		return
	}
	securityParameters, ok := request.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if !ok {
		return
	}
	engineID := labEngineID(host.ID)

	if securityParameters.AuthoritativeEngineID == "" {
		if host.Fault == "disappear" {
			s.mu.Lock()
			s.calls[host.ID]++
			calls := s.calls[host.ID]
			s.mu.Unlock()
			if calls > 1 {
				return
			}
		}
		s.send(conn, addr, reportPacket(request, engineID))
		return
	}

	if host.Fault == "timeout" {
		return
	}
	if host.LatencyMS > 0 {
		time.Sleep(time.Duration(host.LatencyMS) * time.Millisecond)
	}

	switch request.PDUType {
	case gosnmp.GetRequest:
		s.send(conn, addr, getResponse(request, host, engineID))
	case gosnmp.GetBulkRequest, gosnmp.GetNextRequest:
		if host.Fault == "permission_denied" {
			// Simulates a restricted view on the interfaces subtree by
			// dropping the request: the required system group still
			// answers normally, but the optional interface walk times out
			// and is reported as a retryable partial failure rather than
			// failing discovery outright.
			return
		}
		s.send(conn, addr, bulkResponse(request, host, engineID))
	}
}

func (s *SNMPServer) send(conn *net.UDPConn, addr *net.UDPAddr, packet *gosnmp.SnmpPacket) {
	out, err := packet.MarshalMsg()
	if err != nil {
		return
	}
	_, _ = conn.WriteToUDP(out, addr)
}

func decodeSNMPPacket(data []byte) (*gosnmp.SnmpPacket, error) {
	decoder := &gosnmp.GoSNMP{
		Version:            gosnmp.Version3,
		SecurityModel:      gosnmp.UserSecurityModel,
		MsgFlags:           gosnmp.NoAuthNoPriv,
		SecurityParameters: &gosnmp.UsmSecurityParameters{UserName: "topo-lab-decoder"},
	}
	return decoder.SnmpDecodePacket(data)
}

func reportPacket(request *gosnmp.SnmpPacket, engineID string) *gosnmp.SnmpPacket {
	return &gosnmp.SnmpPacket{
		Version:            gosnmp.Version3,
		MsgFlags:           gosnmp.Reportable,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: labSecurityParameters(engineID),
		ContextEngineID:    engineID,
		PDUType:            gosnmp.Report,
		RequestID:          request.RequestID,
		MsgID:              request.MsgID,
	}
}

func getResponse(request *gosnmp.SnmpPacket, host Host, engineID string) *gosnmp.SnmpPacket {
	variables := make([]gosnmp.SnmpPDU, 0, len(request.Variables))
	for _, requested := range request.Variables {
		switch trimOID(requested.Name) {
		case oidSysDescr:
			if host.Fault == "malformed" {
				variables = append(variables, gosnmp.SnmpPDU{Name: oidSysDescr, Type: gosnmp.Integer, Value: 0})
				continue
			}
			variables = append(variables, gosnmp.SnmpPDU{Name: oidSysDescr, Type: gosnmp.OctetString, Value: []byte(fmt.Sprintf("Topo Lab simulated device (%s)", host.Persona))})
		case oidSysObjectID:
			variables = append(variables, gosnmp.SnmpPDU{Name: oidSysObjectID, Type: gosnmp.ObjectIdentifier, Value: "1.3.6.1.4.1.999999.1"})
		case oidSysUpTime:
			variables = append(variables, gosnmp.SnmpPDU{Name: oidSysUpTime, Type: gosnmp.TimeTicks, Value: uint32(time.Now().Unix() % (1 << 32))}) //nolint:gosec
		case oidSysName:
			variables = append(variables, gosnmp.SnmpPDU{Name: oidSysName, Type: gosnmp.OctetString, Value: []byte(host.Name)})
		default:
			variables = append(variables, gosnmp.SnmpPDU{Name: requested.Name, Type: gosnmp.NoSuchObject, Value: nil})
		}
	}
	return &gosnmp.SnmpPacket{
		Version:            gosnmp.Version3,
		MsgFlags:           gosnmp.Reportable,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: labSecurityParameters(engineID),
		ContextEngineID:    engineID,
		PDUType:            gosnmp.GetResponse,
		RequestID:          request.RequestID,
		MsgID:              request.MsgID,
		Variables:          variables,
	}
}

// bulkResponse simulates exactly one ifTable row (a single "primary"
// interface per simulated host, matching the SSH and WinRM Topo Lab
// fixtures) and terminates the walk with EndOfMibView, so the acceptance
// test exercises a real multi-request BulkWalk rather than a single
// round trip.
func bulkResponse(request *gosnmp.SnmpPacket, host Host, engineID string) *gosnmp.SnmpPacket {
	response := &gosnmp.SnmpPacket{
		Version:            gosnmp.Version3,
		MsgFlags:           gosnmp.Reportable,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: labSecurityParameters(engineID),
		ContextEngineID:    engineID,
		PDUType:            gosnmp.GetResponse,
		RequestID:          request.RequestID,
		MsgID:              request.MsgID,
	}
	requestedName := ""
	if len(request.Variables) > 0 {
		requestedName = request.Variables[0].Name
	}
	switch trimOID(requestedName) {
	case oidIfDescr:
		response.Variables = []gosnmp.SnmpPDU{{Name: oidIfDescr + ".1", Type: gosnmp.OctetString, Value: []byte("primary")}}
	case oidIfPhysAddr:
		mac, err := net.ParseMAC(host.MACAddress)
		if err != nil {
			response.Variables = []gosnmp.SnmpPDU{{Name: trimOID(requestedName), Type: gosnmp.EndOfMibView, Value: nil}}
			break
		}
		response.Variables = []gosnmp.SnmpPDU{{Name: oidIfPhysAddr + ".1", Type: gosnmp.OctetString, Value: []byte(mac)}}
	default:
		response.Variables = []gosnmp.SnmpPDU{{Name: trimOID(requestedName), Type: gosnmp.EndOfMibView, Value: nil}}
	}
	return response
}

func labSecurityParameters(engineID string) *gosnmp.UsmSecurityParameters {
	return &gosnmp.UsmSecurityParameters{
		AuthoritativeEngineID:    engineID,
		AuthoritativeEngineBoots: 1,
		AuthoritativeEngineTime:  1,
	}
}

// labEngineID deterministically derives a syntactically valid SNMPv3 engine
// ID (RFC 3411 §5: enterprise-specific format, 5-32 octets) from a Topo Lab
// host ID, so the plugin's engine-ID-derived asset identity is stable
// across repeated scans of the same simulated host.
func labEngineID(hostID string) string {
	suffix := hostID
	if len(suffix) > 26 {
		suffix = suffix[:26]
	}
	return string(append([]byte{0x80, 0x00, 0x00, 0x00, 0x04}, []byte(suffix)...))
}

func trimOID(name string) string {
	return strings.TrimPrefix(name, ".")
}
