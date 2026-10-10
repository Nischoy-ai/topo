package worker

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Nischoy-ai/topo/pkg/credentialref"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
)

// Opt-in compatibility evidence. Normal CI uses fixtures; this test requires
// explicitly authorized targets, independently verified trust and a credential
// reference. It uses the production Executor, without changing installed Topo.
func TestRealManagedWinRMHost(t *testing.T) {
	target := os.Getenv("TOPO_REAL_WINRM_TARGET")
	if target == "" {
		t.Skip("real WinRM target not explicitly configured")
	}
	prefix, err := netip.ParsePrefix(target)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
		t.Fatal("real target must be one canonical IPv4 /32")
	}
	allowlist := filepath.Join(t.TempDir(), "targets.allow")
	if err := os.WriteFile(allowlist, []byte(target+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	startup, err := LoadWinRMStartupConfig(allowlist, os.Getenv("TOPO_REAL_WINRM_CA_CERTS"))
	if err != nil {
		t.Fatal("real trust configuration failed")
	}
	password, err := credentialref.Resolve(os.Getenv("TOPO_REAL_WINRM_PASSWORD_REF"))
	if err != nil {
		t.Fatal("real credential reference could not be resolved")
	}
	source := &testCredentialSource{credential: PasswordCredential{Username: os.Getenv("TOPO_REAL_WINRM_USERNAME"), Password: string(password)}}
	policy := testWinRMPolicy()
	policy.WinRMAllowlist = startup.Allowlist
	policy.WinRMTrustDigest = startup.TrustDigest
	executor := Executor{Policy: policy, WinRMRootCAs: startup.RootCAs}
	var scans []model.ObservationEnvelope
	for range 2 {
		task := testSSHTask(target)
		task.Operation = OperationWinRMWindowsV1
		envelope, err := executor.ExecuteWithCredentials(t.Context(), task, source)
		if err != nil || len(envelope.Errors) != 0 {
			t.Fatal("real managed collection failed; inspect sanitized diagnostics separately")
		}
		var hosts, interfaces int
		for _, asset := range envelope.Assets {
			switch asset.Type {
			case model.AssetHost:
				hosts++
			case model.AssetNetworkInterface:
				interfaces++
			default:
				t.Fatal("unreviewed asset class")
			}
		}
		if hosts != 1 || interfaces < 1 || len(envelope.Relationships) != interfaces {
			t.Fatalf("real inventory counts: hosts=%d interfaces=%d relationships=%d", hosts, interfaces, len(envelope.Relationships))
		}
		payload, err := (servicenow.Publisher{Config: servicenow.Config{InstanceURL: "https://offline-preview.invalid", DiscoverySource: "Nischoy Topo", DryRun: true}}).Preview(t.Context(), []model.ObservationEnvelope{envelope})
		if err != nil {
			t.Fatal("real inventory does not satisfy the reviewed IRE mapping")
		}
		body, _ := json.Marshal(envelope)
		preview, _ := json.Marshal(payload)
		if strings.Contains(string(body), string(password)) || strings.Contains(string(preview), string(password)) {
			t.Fatal("credential leaked into observation or preview")
		}
		scans = append(scans, envelope)
	}
	identities := func(envelope model.ObservationEnvelope) []string {
		var values []string
		for _, asset := range envelope.Assets {
			values = append(values, string(asset.Type)+":"+asset.NativeID)
		}
		for _, relation := range envelope.Relationships {
			values = append(values, relation.Type+":"+relation.FromNativeID+":"+relation.ToNativeID)
		}
		sort.Strings(values)
		return values
	}
	if !reflect.DeepEqual(identities(scans[0]), identities(scans[1])) {
		t.Fatal("real repeated scan changed identity")
	}
	if source.calls != 2 {
		t.Fatal("unexpected credential resolution count")
	}
	t.Logf("real production executor: two scans, %d assets/%d relationships each, zero errors, stable identity; offline IRE preview passed", len(scans[0].Assets), len(scans[0].Relationships))
}

var _ CredentialSource = (*testCredentialSource)(nil)

func TestRealManagedSSHHost(t *testing.T) {
	target := os.Getenv("TOPO_REAL_SSH_TARGET")
	if target == "" {
		t.Skip("real SSH target not explicitly configured")
	}
	allowlist := filepath.Join(t.TempDir(), "targets.allow")
	if err := os.WriteFile(allowlist, []byte(target+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	startup, err := LoadSSHStartupConfig(allowlist, os.Getenv("TOPO_REAL_SSH_KNOWN_HOSTS"))
	if err != nil {
		t.Fatal("real SSH trust configuration failed")
	}
	password, err := credentialref.Resolve(os.Getenv("TOPO_REAL_SSH_PASSWORD_REF"))
	if err != nil {
		t.Fatal("real credential reference could not be resolved")
	}
	source := &testCredentialSource{credential: PasswordCredential{Username: os.Getenv("TOPO_REAL_SSH_USERNAME"), Password: string(password)}}
	policy := testSSHPolicy(startup.Allowlist[0])
	policy.SSHHostKeyDigest = startup.KnownHostsDigest
	executor := Executor{Policy: policy, SSHHostKeyCallback: startup.HostKeyCallback}
	var previous []string
	var assetCount, relationshipCount int
	for repeat := 0; repeat < 2; repeat++ {
		envelope, err := executor.ExecuteWithCredentials(t.Context(), testSSHTask(target), source)
		if err != nil || len(envelope.Errors) != 0 {
			t.Fatal("real SSH collection failed; inspect sanitized diagnostics separately")
		}
		if len(envelope.Assets) < 2 || len(envelope.Relationships) != len(envelope.Assets)-1 {
			t.Fatal("real SSH host/interface inventory is incomplete")
		}
		payload, err := (servicenow.Publisher{Config: servicenow.Config{InstanceURL: "https://offline-preview.invalid", DiscoverySource: "Nischoy Topo", DryRun: true}}).Preview(t.Context(), []model.ObservationEnvelope{envelope})
		if err != nil {
			t.Fatal("real SSH inventory failed offline IRE preview")
		}
		body, _ := json.Marshal(envelope)
		preview, _ := json.Marshal(payload)
		if strings.Contains(string(body), string(password)) || strings.Contains(string(preview), string(password)) {
			t.Fatal("credential leaked")
		}
		var identities []string
		for _, asset := range envelope.Assets {
			identities = append(identities, string(asset.Type)+":"+asset.NativeID)
		}
		for _, relation := range envelope.Relationships {
			identities = append(identities, relation.Type+":"+relation.FromNativeID+":"+relation.ToNativeID)
		}
		sort.Strings(identities)
		if repeat == 1 && !reflect.DeepEqual(previous, identities) {
			t.Fatal("real SSH repeat changed identity")
		}
		previous = identities
		assetCount = len(envelope.Assets)
		relationshipCount = len(envelope.Relationships)
	}
	t.Logf("real production SSH executor: two scans, %d assets/%d relationships each, zero errors, stable identity; offline IRE preview passed", assetCount, relationshipCount)
}
