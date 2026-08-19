package snmp

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func validRequest() discovery.Request {
	return discovery.Request{Targets: []string{"127.0.0.1:1161"}}
}

func TestValidateConfigurationRequiresTargets(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true}}
	if err := p.ValidateConfiguration(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestValidateConfigurationRequiresUsername(t *testing.T) {
	p := Plugin{Config: Config{SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing username")
	}
}

func TestValidateConfigurationRequiresSecurityLevel(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", LabMode: true}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing security level")
	}
}

func TestValidateConfigurationRejectsNoAuthNoPrivOutsideLabMode(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for noAuthNoPriv outside LabMode")
	}
}

func TestValidateConfigurationRejectsAuthPrivInsideLabMode(t *testing.T) {
	p := Plugin{Config: Config{
		Username: "u", SecurityLevel: SecurityLevelAuthPriv, LabMode: true,
		AuthProtocol: AuthProtocolSHA, AuthPassphrase: "password123",
		PrivProtocol: PrivProtocolAES, PrivPassphrase: "password123",
	}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for authPriv inside LabMode")
	}
}

func TestValidateConfigurationRejectsAuthNoPriv(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelAuthNoPriv}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for authNoPriv")
	}
}

func TestValidateConfigurationRequiresSHAAndAESInProduction(t *testing.T) {
	base := Config{Username: "u", SecurityLevel: SecurityLevelAuthPriv, AuthPassphrase: "password123", PrivPassphrase: "password123"}
	cases := []Config{
		func() Config { c := base; c.AuthProtocol = AuthProtocolMD5; c.PrivProtocol = PrivProtocolAES; return c }(),
		func() Config { c := base; c.AuthProtocol = AuthProtocolSHA; c.PrivProtocol = PrivProtocolDES; return c }(),
	}
	for _, cfg := range cases {
		p := Plugin{Config: cfg}
		if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
			t.Fatalf("expected error for config %#v", cfg)
		}
	}
}

func TestValidateConfigurationRequiresLongEnoughPassphrases(t *testing.T) {
	p := Plugin{Config: Config{
		Username: "u", SecurityLevel: SecurityLevelAuthPriv,
		AuthProtocol: AuthProtocolSHA, AuthPassphrase: "short",
		PrivProtocol: PrivProtocolAES, PrivPassphrase: "password123",
	}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for short authentication passphrase")
	}
}

func TestValidateConfigurationAcceptsValidProductionConfig(t *testing.T) {
	p := Plugin{Config: Config{
		Username: "u", SecurityLevel: SecurityLevelAuthPriv,
		AuthProtocol: AuthProtocolSHA, AuthPassphrase: "password123",
		PrivProtocol: PrivProtocolAES, PrivPassphrase: "password123",
	}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigurationAcceptsValidLabConfig(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfigurationRejectsSecretLikeOptions(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true}}
	req := validRequest()
	req.Options = map[string]string{"priv_password": "x"}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for secret-like option key")
	}
}

func TestValidateConfigurationRejectsNonLoopbackInLabMode(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true}}
	req := discovery.Request{Targets: []string{"192.0.2.1:161"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-loopback Topo Lab target")
	}
}

func TestParseTargetRequiresPort(t *testing.T) {
	if _, _, err := parseTarget("127.0.0.1", false); err == nil {
		t.Fatal("expected error for missing port")
	}
}

func TestParseTargetAcceptsHostPort(t *testing.T) {
	host, port, err := parseTarget("192.0.2.1:1161", false)
	if err != nil {
		t.Fatal(err)
	}
	if host != "192.0.2.1" || port != 1161 {
		t.Fatalf("got host=%q port=%d", host, port)
	}
}

func TestDiscoverFailsFastOnInvalidConfiguration(t *testing.T) {
	p := Plugin{Config: Config{}}
	if _, err := p.Discover(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDiscoverRespectsContextCancellation(t *testing.T) {
	p := Plugin{Config: Config{Username: "u", SecurityLevel: SecurityLevelNoAuthNoPriv, LabMode: true, Timeout: 50 * time.Millisecond, Retries: 0}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := discovery.Request{Targets: []string{"127.0.0.1:1", "127.0.0.1:2"}}
	if _, err := p.Discover(ctx, req); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
