package aws

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
)

func validRequest() discovery.Request {
	return discovery.Request{Targets: []string{"https://organizations.us-east-1.amazonaws.com"}}
}

func validPlugin() Plugin {
	return Plugin{Config: Config{AccessKeyID: "AKIAEXAMPLE0000000", SecretAccessKey: "hunter2hunter2hunter2secretkey", Region: "us-east-1"}}
}

func TestValidateConfigurationRequiresTargets(t *testing.T) {
	p := validPlugin()
	if err := p.ValidateConfiguration(context.Background(), discovery.Request{}); err == nil {
		t.Fatal("expected error for empty targets")
	}
}

func TestValidateConfigurationRequiresCredentials(t *testing.T) {
	p := Plugin{Config: Config{Region: "us-east-1"}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing access key/secret")
	}
}

func TestValidateConfigurationRequiresRegion(t *testing.T) {
	p := Plugin{Config: Config{AccessKeyID: "AKIAEXAMPLE0000000", SecretAccessKey: "hunter2hunter2hunter2secretkey"}}
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for missing region")
	}
}

func TestValidateConfigurationRejectsControlCharacterAccessKeyID(t *testing.T) {
	p := validPlugin()
	p.Config.AccessKeyID = "AKIA\r\nEXAMPLE"
	if err := p.ValidateConfiguration(context.Background(), validRequest()); err == nil {
		t.Fatal("expected error for control character in access key ID")
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
	req := discovery.Request{Targets: []string{"http://organizations.us-east-1.amazonaws.com"}}
	if err := p.ValidateConfiguration(context.Background(), req); err == nil {
		t.Fatal("expected error for non-HTTPS target outside Topo Lab mode")
	}
}

func TestValidateConfigurationRejectsNonLoopbackInLabMode(t *testing.T) {
	p := validPlugin()
	p.Config.LabMode = true
	req := discovery.Request{Targets: []string{"https://organizations.us-east-1.amazonaws.com"}}
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
	if _, err := validateTarget("https://user:pass@organizations.us-east-1.amazonaws.com", false); err == nil {
		t.Fatal("expected error for embedded credentials")
	}
}

func TestValidateTargetRejectsQueryAndFragment(t *testing.T) {
	if _, err := validateTarget("https://organizations.us-east-1.amazonaws.com?x=1", false); err == nil {
		t.Fatal("expected error for query string")
	}
	if _, err := validateTarget("https://organizations.us-east-1.amazonaws.com#frag", false); err == nil {
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
