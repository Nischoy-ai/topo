package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func validRequest() discovery.Request {
	return discovery.Request{Targets: []string{"https://api.example.test"}}
}

func validPlugin() Plugin {
	return Plugin{Config: Config{BearerToken: "hunter2hunter2token"}}
}

func TestValidateConfigurationRequiresTargets(t *testing.T) {
	p := validPlugin()
	if err := p.ValidateConfiguration(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestValidateConfigurationRequiresBearerToken(t *testing.T) {
	p := Plugin{}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing bearer token")
	}
}

func TestValidateConfigurationRejectsControlCharacterToken(t *testing.T) {
	p := Plugin{Config: Config{BearerToken: "token\r\nwith-crlf"}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for control character in token")
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

func TestValidateConfigurationRejectsHTTPOutsideLabMode(t *testing.T) {
	p := validPlugin()
	req := discovery.Request{Targets: []string{"http://api.example.test"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-HTTPS target outside Topo Lab mode")
	}
}

func TestValidateConfigurationRejectsNonLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"https://api.example.test"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-loopback Topo Lab target")
	}
}

func TestValidateConfigurationAcceptsLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"http://127.0.0.1:6443"}}
	if err := p.ValidateConfiguration(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTargetRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := validateTarget("https://user:pass@api.example.test", false); err == nil {
		t.Fatal("expected error for embedded credentials")
	}
}

func TestValidateTargetRejectsQueryAndFragment(t *testing.T) {
	if _, err := validateTarget("https://api.example.test?x=1", false); err == nil {
		t.Fatal("expected error for query string")
	}
	if _, err := validateTarget("https://api.example.test#frag", false); err == nil {
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
	p.Config.OperationTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := discovery.Request{Targets: []string{"http://127.0.0.1:1", "http://127.0.0.1:2"}}
	if _, err := p.Discover(ctx, req); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
