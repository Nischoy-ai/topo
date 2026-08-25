package azure

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func validRequest() discovery.Request {
	return discovery.Request{Targets: []string{"https://management.azure.com"}}
}

func validPlugin() Plugin {
	return Plugin{Config: Config{
		TenantID: "11111111-1111-1111-1111-111111111111", ClientID: "22222222-2222-2222-2222-222222222222",
		ClientSecret: "hunter2hunter2hunter2clientsecret", AuthorityURL: "https://login.microsoftonline.com",
	}}
}

func TestValidateConfigurationRequiresTargets(t *testing.T) {
	p := validPlugin()
	if err := p.ValidateConfiguration(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestValidateConfigurationRequiresCredentials(t *testing.T) {
	p := Plugin{Config: Config{AuthorityURL: "https://login.microsoftonline.com"}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing tenant/client ID or secret")
	}
}

func TestValidateConfigurationRequiresAuthorityURL(t *testing.T) {
	p := validPlugin()
	p.Config.AuthorityURL = ""
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing authority URL")
	}
}

func TestValidateConfigurationRejectsControlCharacterTenantID(t *testing.T) {
	p := validPlugin()
	p.Config.TenantID = "tenant\r\nid"
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for control character in tenant ID")
	}
}

func TestValidateConfigurationRejectsSecretLikeOptions(t *testing.T) {
	p := validPlugin()
	req := validRequest()
	req.Options = map[string]string{"api_token": "x"}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for secret-like option key")
	}
}

func TestValidateConfigurationRejectsHTTPTarget(t *testing.T) {
	p := validPlugin()
	req := discovery.Request{Targets: []string{"http://management.azure.com"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-HTTPS target — Azure never permits plain HTTP, even in Topo Lab mode")
	}
}

func TestValidateConfigurationRejectsNonLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"https://management.azure.com"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-loopback Topo Lab target")
	}
}

func TestValidateConfigurationAcceptsLoopbackHTTPSInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	p.Config.AuthorityURL = "https://127.0.0.1:6443"
	req := discovery.Request{Targets: []string{"https://127.0.0.1:6443"}}
	if err := p.ValidateConfiguration(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTargetRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := validateTarget("https://user:pass@management.azure.com", false); err == nil {
		t.Fatal("expected error for embedded credentials")
	}
}

func TestValidateTargetRejectsQueryAndFragment(t *testing.T) {
	if _, err := validateTarget("https://management.azure.com?x=1", false); err == nil {
		t.Fatal("expected error for query string")
	}
	if _, err := validateTarget("https://management.azure.com#frag", false); err == nil {
		t.Fatal("expected error for fragment")
	}
}

func TestDiscoverFailsFastOnInvalidConfiguration(t *testing.T) {
	p := Plugin{}
	if _, err := p.Discover(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDiscoverRespectsContextCancellation(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	p.Config.AuthorityURL = "https://127.0.0.1:1"
	p.Config.OperationTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := discovery.Request{Targets: []string{"https://127.0.0.1:1", "https://127.0.0.1:2"}}
	if _, err := p.Discover(ctx, req); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
