package vmware

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func validRequest() discovery.Request {
	return discovery.Request{Targets: []string{"https://vcenter.example.test"}}
}

func validPlugin() Plugin {
	return Plugin{Config: Config{Username: "reader", Password: "hunter2hunter2"}}
}

func TestValidateConfigurationRequiresTargets(t *testing.T) {
	p := validPlugin()
	if err := p.ValidateConfiguration(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestValidateConfigurationRequiresCredentials(t *testing.T) {
	for _, p := range []Plugin{
		{Config: Config{Password: "hunter2hunter2"}},
		{Config: Config{Username: "reader"}},
	} {
		if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
			t.Fatalf("expected error for incomplete credentials: %#v", p.Config)
		}
	}
}

func TestValidateConfigurationRejectsSecretLikeOptions(t *testing.T) {
	p := validPlugin()
	req := validRequest()
	req.Options = map[string]string{"vcenter_password": "x"}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for secret-like option key")
	}
}

func TestValidateConfigurationRejectsHTTPOutsideLabMode(t *testing.T) {
	p := validPlugin()
	req := discovery.Request{Targets: []string{"http://vcenter.example.test"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-HTTPS target outside Topo Lab mode")
	}
}

func TestValidateConfigurationRejectsNonLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"https://vcenter.example.test"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-loopback Topo Lab target")
	}
}

func TestValidateConfigurationAcceptsLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"https://127.0.0.1:8443"}}
	if err := p.ValidateConfiguration(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTargetRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := validateTarget("https://user:pass@vcenter.example.test", false); err == nil {
		t.Fatal("expected error for embedded credentials")
	}
}

func TestValidateTargetRejectsQueryAndFragment(t *testing.T) {
	if _, err := validateTarget("https://vcenter.example.test/sdk?x=1", false); err == nil {
		t.Fatal("expected error for query string")
	}
	if _, err := validateTarget("https://vcenter.example.test/sdk#frag", false); err == nil {
		t.Fatal("expected error for fragment")
	}
}

func TestValidateTargetDefaultsSDKPath(t *testing.T) {
	target, err := validateTarget("https://vcenter.example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != "/sdk" {
		t.Fatalf("got path %q, want /sdk", target.Path)
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
	p.Config.ConnectTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := discovery.Request{Targets: []string{"https://127.0.0.1:1", "https://127.0.0.1:2"}}
	if _, err := p.Discover(ctx, req); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
