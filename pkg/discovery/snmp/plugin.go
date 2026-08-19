// Package snmp discovers network equipment identity and interfaces over
// SNMPv3, using the MIB-II system and interfaces groups.
package snmp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/gosnmp/gosnmp"
)

const (
	SecurityLevelNoAuthNoPriv = "noAuthNoPriv"
	SecurityLevelAuthNoPriv   = "authNoPriv"
	SecurityLevelAuthPriv     = "authPriv"

	AuthProtocolSHA = "SHA"
	AuthProtocolMD5 = "MD5"

	PrivProtocolAES = "AES"
	PrivProtocolDES = "DES"
)

// Config configures the SNMP plugin. Production discovery requires
// SecurityLevel authPriv with SHA authentication and AES privacy, mirroring
// how the WinRM plugin's production path requires NTLM+HTTPS and only
// permits a weaker mode inside explicit LabMode. Topo Lab's SNMP agent
// supports noAuthNoPriv only, since gosnmp is client-only and there is no
// off-the-shelf SNMP agent simulator to reuse for a from-scratch USM
// (RFC 3414/3826) server-side implementation.
type Config struct {
	Username                       string
	SecurityLevel                  string
	AuthProtocol, PrivProtocol     string
	AuthPassphrase, PrivPassphrase string
	LabMode                        bool
	Concurrency                    int
	Timeout                        time.Duration
	Retries                        int
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "snmp-network",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetHost, model.AssetNetworkInterface},
		RequiredPermissions: []string{
			"SNMPv3 read access to the MIB-II system group (sysDescr, sysObjectID, sysUpTime, sysName)",
			"SNMPv3 read access to the MIB-II interfaces group (ifTable: ifDescr, ifPhysAddress)",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one SNMP target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("SNMP concurrency must be between 0 (default) and 1024")
	}
	if p.Config.Timeout < 0 {
		return errors.New("SNMP timeout cannot be negative")
	}
	if p.Config.Retries < 0 || p.Config.Retries > 10 {
		return errors.New("SNMP retries must be between 0 and 10")
	}
	for key := range r.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("SNMP secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.Username == "" {
		return errors.New("SNMPv3 username is required")
	}
	if len(p.Config.Username) > 512 {
		return errors.New("SNMP username exceeds 512 bytes")
	}
	if strings.ContainsAny(p.Config.Username, "\x00\r\n") {
		return errors.New("SNMP username contains a control character")
	}
	if len(p.Config.AuthPassphrase) > 4096 || len(p.Config.PrivPassphrase) > 4096 {
		return errors.New("SNMP passphrase exceeds 4096 bytes")
	}
	switch p.Config.SecurityLevel {
	case "":
		return errors.New("SNMP security level is required")
	case SecurityLevelNoAuthNoPriv:
		if !p.Config.LabMode {
			return errors.New("SNMP noAuthNoPriv is only permitted in Topo Lab mode; production requires authPriv")
		}
		if p.Config.AuthProtocol != "" || p.Config.PrivProtocol != "" || p.Config.AuthPassphrase != "" || p.Config.PrivPassphrase != "" {
			return errors.New("SNMP noAuthNoPriv cannot be combined with an authentication or privacy protocol")
		}
	case SecurityLevelAuthNoPriv:
		return errors.New("SNMP authNoPriv is not supported; use authPriv")
	case SecurityLevelAuthPriv:
		if p.Config.LabMode {
			return errors.New("Topo Lab SNMP agent only supports noAuthNoPriv")
		}
		if p.Config.AuthProtocol != AuthProtocolSHA {
			return fmt.Errorf("SNMP authPriv requires SHA authentication, got %q", p.Config.AuthProtocol)
		}
		if p.Config.PrivProtocol != PrivProtocolAES {
			return fmt.Errorf("SNMP authPriv requires AES privacy, got %q", p.Config.PrivProtocol)
		}
		if len(p.Config.AuthPassphrase) < 8 {
			return errors.New("SNMP authentication passphrase must be at least 8 characters")
		}
		if len(p.Config.PrivPassphrase) < 8 {
			return errors.New("SNMP privacy passphrase must be at least 8 characters")
		}
	default:
		return fmt.Errorf("unsupported SNMP security level %q", p.Config.SecurityLevel)
	}
	for _, raw := range r.Targets {
		if _, _, err := parseTarget(raw, p.Config.LabMode); err != nil {
			return err
		}
	}
	return nil
}

func (p Plugin) CheckConnectivity(ctx context.Context, r discovery.Request) error {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return err
	}
	client, err := p.dial(ctx, r.Targets[0])
	if err != nil {
		return err
	}
	defer client.Conn.Close()
	_, err = client.Get([]string{oidSysDescr})
	return err
}

func (p Plugin) Discover(ctx context.Context, r discovery.Request) (model.ObservationEnvelope, error) {
	if err := p.ValidateConfiguration(ctx, r); err != nil {
		return model.ObservationEnvelope{}, err
	}
	now := time.Now().UTC()
	obs := model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: observationID(),
		SiteID:        valueOr(r.SiteID, "default"),
		CollectorID:   valueOr(r.CollectorID, "snmp-relay"),
		Plugin:        "snmp-network",
		JobID:         r.JobID,
		ObservedAt:    now,
	}
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 32
	}
	jobs := make(chan string)
	var workers sync.WaitGroup
	var mu sync.Mutex
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for target := range jobs {
				inventory, collectionErrors := p.discoverTarget(ctx, target)
				mu.Lock()
				if inventory != nil {
					assets, relationships := inventory.Assets(now)
					obs.Assets = append(obs.Assets, assets...)
					obs.Relationships = append(obs.Relationships, relationships...)
				}
				obs.Errors = append(obs.Errors, collectionErrors...)
				mu.Unlock()
			}
		}()
	}
	for _, target := range r.Targets {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return model.ObservationEnvelope{}, ctx.Err()
		case jobs <- target:
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return model.ObservationEnvelope{}, err
	}
	return obs, nil
}

func (p Plugin) discoverTarget(ctx context.Context, rawTarget string) (*Inventory, []model.CollectionError) {
	client, err := p.dial(ctx, rawTarget)
	if err != nil {
		return nil, []model.CollectionError{{Code: "snmp_connect", Message: rawTarget + ": " + err.Error(), Retryable: retryable(err)}}
	}
	defer client.Conn.Close()

	packet, err := client.Get([]string{oidSysDescr, oidSysObjectID, oidSysUpTime, oidSysName})
	if err != nil {
		return nil, []model.CollectionError{{Code: "snmp_operation", Message: rawTarget + ": system group: " + err.Error(), Retryable: retryable(err)}}
	}
	sysDescr, sysObjectID, sysName, sysUpTime, err := parseSystemGroup(packet.Variables)
	if err != nil {
		return nil, []model.CollectionError{{Code: "snmp_parse", Message: rawTarget + ": " + err.Error()}}
	}
	if sysName == "" {
		return nil, []model.CollectionError{{Code: "snmp_parse", Message: rawTarget + ": empty sysName"}}
	}

	usm, ok := client.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if !ok || usm.AuthoritativeEngineID == "" {
		return nil, []model.CollectionError{{Code: "snmp_parse", Message: rawTarget + ": missing SNMPv3 engine ID"}}
	}
	engineID := hex.EncodeToString([]byte(usm.AuthoritativeEngineID))

	var collectionErrors []model.CollectionError
	interfaces, err := p.walkInterfaces(client)
	if err != nil {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "snmp_partial", Message: rawTarget + ": interfaces: " + err.Error(), Retryable: retryable(err)})
	}

	inventory := &Inventory{
		EngineID:       engineID,
		SysName:        sysName,
		SysDescr:       sysDescr,
		SysObjectID:    sysObjectID,
		SysUpTimeTicks: sysUpTime,
		Interfaces:     interfaces,
	}
	return inventory, collectionErrors
}

func (p Plugin) dial(ctx context.Context, rawTarget string) (*gosnmp.GoSNMP, error) {
	host, port, err := parseTarget(rawTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	timeout := p.Config.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	retries := p.Config.Retries
	if retries <= 0 {
		retries = 1
	}
	var (
		msgFlags gosnmp.SnmpV3MsgFlags
		params   *gosnmp.UsmSecurityParameters
	)
	switch p.Config.SecurityLevel {
	case SecurityLevelNoAuthNoPriv:
		msgFlags = gosnmp.NoAuthNoPriv
		params = &gosnmp.UsmSecurityParameters{UserName: p.Config.Username}
	case SecurityLevelAuthPriv:
		msgFlags = gosnmp.AuthPriv
		params = &gosnmp.UsmSecurityParameters{
			UserName:                 p.Config.Username,
			AuthenticationProtocol:   authProtocol(p.Config.AuthProtocol),
			AuthenticationPassphrase: p.Config.AuthPassphrase,
			PrivacyProtocol:          privProtocol(p.Config.PrivProtocol),
			PrivacyPassphrase:        p.Config.PrivPassphrase,
		}
	default:
		return nil, fmt.Errorf("unsupported SNMP security level %q", p.Config.SecurityLevel)
	}
	client := &gosnmp.GoSNMP{
		Target:             host,
		Port:               port,
		Version:            gosnmp.Version3,
		SecurityModel:      gosnmp.UserSecurityModel,
		MsgFlags:           msgFlags,
		SecurityParameters: params,
		Timeout:            timeout,
		Retries:            retries,
		Context:            ctx,
	}
	if err := client.Connect(); err != nil {
		return nil, err
	}
	return client, nil
}

func authProtocol(name string) gosnmp.SnmpV3AuthProtocol {
	switch name {
	case AuthProtocolSHA:
		return gosnmp.SHA
	case AuthProtocolMD5:
		return gosnmp.MD5
	default:
		return gosnmp.NoAuth
	}
}

func privProtocol(name string) gosnmp.SnmpV3PrivProtocol {
	switch name {
	case PrivProtocolAES:
		return gosnmp.AES
	case PrivProtocolDES:
		return gosnmp.DES
	default:
		return gosnmp.NoPriv
	}
}

func parseTarget(raw string, labMode bool) (host string, port uint16, err error) {
	if len(raw) > 2048 {
		return "", 0, errors.New("SNMP target exceeds 2048 bytes")
	}
	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		return "", 0, fmt.Errorf("invalid SNMP target %q: %w", raw, err)
	}
	parsedPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", 0, fmt.Errorf("invalid SNMP target port in %q", raw)
	}
	if labMode {
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", 0, fmt.Errorf("Topo Lab SNMP target %q must be loopback", raw)
		}
	}
	return host, uint16(parsedPort), nil
}

func retryable(err error) bool {
	var netError net.Error
	if errors.As(err, &netError) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// gosnmp reports its own request/retry timeout as a plain fmt.Errorf
	// ("request timeout (after N retries)"), not a net.Error or a wrapped
	// context error, so it would otherwise be misclassified as permanent.
	return strings.Contains(err.Error(), "timeout")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func observationID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
