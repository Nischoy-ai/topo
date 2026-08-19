// Package vmware discovers VMware vCenter (or standalone ESXi) virtual
// machine and host inventory over the vSphere API, using only read-only
// enumeration — it never issues a power, configuration, or lifecycle
// operation against a managed object.
package vmware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
)

// maxManagedObjects bounds each of the host and virtual machine listings so
// a runaway or hostile vCenter cannot force unbounded memory use, matching
// the WinRM enumeration object cap and the SNMP interface-table walk cap.
const maxManagedObjects = 100000

type Config struct {
	Username, Password string
	// LabMode permits skipping vCenter/ESXi TLS certificate verification and
	// relaxes the HTTPS-only requirement, restricted to loopback Topo Lab
	// (vcsim) targets. Production targets always verify the presented
	// certificate.
	LabMode                          bool
	Concurrency                      int
	ConnectTimeout, OperationTimeout time.Duration
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "vmware-vcenter",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetHost, model.AssetVirtualMachine, model.AssetNetworkInterface},
		RequiredPermissions: []string{
			"read-only vSphere API access to enumerate HostSystem and VirtualMachine inventory (a read-only vCenter role is sufficient; no configuration, power, or lifecycle privileges are used)",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one vCenter target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("vCenter concurrency must be between 0 (default) and 1024")
	}
	if p.Config.ConnectTimeout < 0 || p.Config.OperationTimeout < 0 {
		return errors.New("vCenter timeouts cannot be negative")
	}
	for key := range r.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("vCenter secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.Username == "" || p.Config.Password == "" {
		return errors.New("vCenter username and password are required")
	}
	if len(p.Config.Username) > 512 {
		return errors.New("vCenter username exceeds 512 bytes")
	}
	if len(p.Config.Password) > 4096 {
		return errors.New("vCenter password exceeds 4096 bytes")
	}
	if strings.ContainsAny(p.Config.Username, "\x00\r\n") {
		return errors.New("vCenter username contains a control character")
	}
	for _, raw := range r.Targets {
		if _, err := validateTarget(raw, p.Config.LabMode); err != nil {
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
	logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = client.Logout(logoutCtx)
	return nil
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
		CollectorID:   valueOr(r.CollectorID, "vmware-relay"),
		Plugin:        "vmware-vcenter",
		JobID:         r.JobID,
		ObservedAt:    now,
	}
	concurrency := p.Config.Concurrency
	if concurrency < 1 {
		concurrency = 8
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
		return nil, []model.CollectionError{{Code: "vmware_connect", Message: rawTarget + ": " + err.Error(), Retryable: retryable(err)}}
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
	}()

	timeout := p.operationTimeout()
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	manager := view.NewManager(client.Client)
	containerView, err := manager.CreateContainerView(opCtx, client.ServiceContent.RootFolder, []string{"HostSystem", "VirtualMachine"}, true)
	if err != nil {
		return nil, []model.CollectionError{{Code: "vmware_operation", Message: rawTarget + ": create inventory view: " + err.Error(), Retryable: retryable(err)}}
	}
	defer func() {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = containerView.Destroy(destroyCtx)
	}()

	var hosts []mo.HostSystem
	if err := containerView.Retrieve(opCtx, []string{"HostSystem"}, []string{"name", "summary", "config.network"}, &hosts); err != nil {
		return nil, []model.CollectionError{{Code: "vmware_operation", Message: rawTarget + ": list hosts: " + err.Error(), Retryable: retryable(err)}}
	}
	if len(hosts) > maxManagedObjects {
		return nil, []model.CollectionError{{Code: "vmware_operation", Message: fmt.Sprintf("%s: host inventory exceeded %d objects", rawTarget, maxManagedObjects)}}
	}
	hostInventories, hostsByMoRef := mapHosts(hosts)

	var collectionErrors []model.CollectionError
	var vmInventories []VMInventory
	var vms []mo.VirtualMachine
	if err := containerView.Retrieve(opCtx, []string{"VirtualMachine"}, []string{"name", "summary", "config.hardware.device"}, &vms); err != nil {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "vmware_partial", Message: rawTarget + ": list virtual machines: " + err.Error(), Retryable: retryable(err)})
	} else if len(vms) > maxManagedObjects {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "vmware_partial", Message: fmt.Sprintf("%s: virtual machine inventory exceeded %d objects", rawTarget, maxManagedObjects)})
	} else {
		vmInventories = mapVMs(vms, hostsByMoRef)
	}

	return &Inventory{Hosts: hostInventories, VMs: vmInventories}, collectionErrors
}

func (p Plugin) dial(ctx context.Context, rawTarget string) (*govmomi.Client, error) {
	target, err := validateTarget(rawTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	connectTimeout := p.Config.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	loginTarget := *target
	loginTarget.User = nil // credentials are supplied explicitly below, never embedded in the target
	client, err := govmomi.NewClient(connectCtx, &loginTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	if err := client.Login(connectCtx, url.UserPassword(p.Config.Username, p.Config.Password)); err != nil {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
		return nil, err
	}
	return client, nil
}

func (p Plugin) operationTimeout() time.Duration {
	if p.Config.OperationTimeout > 0 {
		return p.Config.OperationTimeout
	}
	return 30 * time.Second
}

func validateTarget(raw string, labMode bool) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errors.New("vCenter target exceeds 2048 bytes")
	}
	parsed, err := soap.ParseURL(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid vCenter target %q", raw)
	}
	// soap.ParseURL always sets a non-nil Userinfo (url.UserPassword("", ""))
	// when the target has none of its own, so presence alone is not a
	// signal — only a non-empty username or password means the target
	// actually embedded credentials.
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	if username != "" || password != "" {
		return nil, fmt.Errorf("vCenter target %q must not contain credentials", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("vCenter target %q must not contain a query or fragment", raw)
	}
	if labMode {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("Topo Lab vCenter target %q must use HTTP or HTTPS", raw)
		}
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("Topo Lab vCenter target %q must be loopback", raw)
		}
	} else if parsed.Scheme != "https" {
		return nil, fmt.Errorf("vCenter target %q must use HTTPS", raw)
	}
	return parsed, nil
}

func retryable(err error) bool {
	var netError net.Error
	if errors.As(err, &netError) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
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
