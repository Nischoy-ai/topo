// Package azure discovers an Azure tenant's own subscription structure —
// which subscriptions exist and how they are grouped into management
// groups under the tenant's root management group — over the real Azure
// Resource Manager (ARM) API, using only read-only Get/List calls. It
// never issues a create, move, or delete operation against any tenant,
// management group, or subscription object.
package azure

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/managementgroups/armmanagementgroups"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
)

// maxObjects bounds the total number of management groups and
// subscriptions collected for a single target, matching the AWS
// root/OU/account cap, the Kubernetes node/pod cap, the VMware
// managed-object cap, and the SNMP interface-table walk cap. This slice
// does not implement chunked pagination beyond that single bound — a
// tenant with more objects than the cap is reported as a collection error
// rather than silently truncated.
const maxObjects = 100000

// maxManagementGroupDepth bounds management-group recursion to 6 levels
// below the tenant root group. This is not an arbitrary safety margin:
// Azure enforces a real 6-level management-group nesting limit, so a
// conformant API can never require deeper recursion. The bound still
// exists in code — rather than trusted implicitly — for the same
// defense-in-depth reason AWS's OU-depth bound does.
const maxManagementGroupDepth = 6

type Config struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	// AuthorityURL is the Azure AD (Microsoft Entra ID) OAuth2 authority
	// base, for example https://login.microsoftonline.com for Azure Public
	// Cloud. Required and never defaulted or autodetected — sovereign
	// clouds (Azure Government, Azure China) use different authority and
	// ARM hosts, and Topo never guesses which one a caller means.
	AuthorityURL string
	// LabMode restricts targets and the authority to loopback and skips
	// TLS certificate verification against them, the same as VMware's and
	// WinRM's own -lab modes. Unlike Kubernetes's and AWS's Lab modes,
	// this cannot fall back to plain HTTP: azidentity's own token-request
	// code unconditionally rejects a non-HTTPS authority host, regardless
	// of any client option, so Topo Lab's Azure fixture is always HTTPS
	// with a self-signed certificate instead.
	LabMode     bool
	Concurrency int
	// OperationTimeout bounds the full token-acquisition-plus-Get/List call
	// sequence for one target, the same per-target bound Kubernetes and AWS
	// use for their own multi-call sequences.
	OperationTimeout time.Duration
}

type client struct {
	managementGroups *armmanagementgroups.Client
	subscriptions    *armsubscriptions.Client
	tenants          *armsubscriptions.TenantsClient
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "azure-tenant",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetCloudResource},
		RequiredPermissions: []string{
			"read-only Azure RBAC access to list the tenant's management groups (with children), subscriptions, and tenant details (the built-in Reader role at the tenant root management group scope is sufficient; no write, move, or delete permission is ever used)",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one Azure Resource Manager endpoint target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("Azure concurrency must be between 0 (default) and 1024")
	}
	if p.Config.OperationTimeout < 0 {
		return errors.New("Azure operation timeout cannot be negative")
	}
	for key := range r.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("Azure secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.TenantID == "" || p.Config.ClientID == "" || p.Config.ClientSecret == "" {
		return errors.New("Azure tenant ID, client ID, and client secret are required")
	}
	if len(p.Config.TenantID) > 128 || len(p.Config.ClientID) > 128 {
		return errors.New("Azure tenant ID or client ID exceeds 128 bytes")
	}
	if len(p.Config.ClientSecret) > 4096 {
		return errors.New("Azure client secret exceeds 4096 bytes")
	}
	if strings.ContainsAny(p.Config.TenantID, "\x00\r\n") || strings.ContainsAny(p.Config.ClientID, "\x00\r\n") {
		return errors.New("Azure tenant ID or client ID contains a control character")
	}
	if p.Config.AuthorityURL == "" {
		return errors.New("Azure authority URL is required")
	}
	if _, err := validateTarget(p.Config.AuthorityURL, p.Config.LabMode); err != nil {
		return fmt.Errorf("Azure authority URL: %w", err)
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
	c, err := p.dial(r.Targets[0])
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, p.operationTimeout())
	defer cancel()
	pager := c.tenants.NewListPager(nil)
	_, err = pager.NextPage(opCtx)
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
		CollectorID:   valueOr(r.CollectorID, "azure-relay"),
		Plugin:        "azure-tenant",
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
	c, err := p.dial(rawTarget)
	if err != nil {
		return nil, []model.CollectionError{{Code: "azure_connect", Message: rawTarget + ": " + err.Error(), Retryable: retryable(err)}}
	}

	opCtx, cancel := context.WithTimeout(ctx, p.operationTimeout())
	defer cancel()

	tenant, err := findTenant(opCtx, c.tenants, p.Config.TenantID)
	if err != nil {
		return nil, []model.CollectionError{{Code: "azure_operation", Message: rawTarget + ": list tenants: " + err.Error(), Retryable: retryable(err)}}
	}

	root, err := c.managementGroups.Get(opCtx, p.Config.TenantID, &armmanagementgroups.ClientGetOptions{
		Expand:  to.Ptr(armmanagementgroups.ManagementGroupExpandTypeChildren),
		Recurse: to.Ptr(true),
	})
	if err != nil {
		return nil, []model.CollectionError{{Code: "azure_operation", Message: rawTarget + ": get management group tree: " + err.Error(), Retryable: retryable(err)}}
	}

	inv := &Inventory{Tenant: tenant}
	objectCount := 0
	rootARMID := valueOfString(root.ID)
	inv.ManagementGroups = append(inv.ManagementGroups, ManagementGroupInfo{
		ARMID: rootARMID, ShortID: valueOfString(root.Name), DisplayName: displayName(root.Properties), ParentID: tenant.ID,
	})
	objectCount++

	var collectionErrors []model.CollectionError
	if root.Properties != nil {
		for _, child := range root.Properties.Children {
			collectionErrors = append(collectionErrors, walk(child, rootARMID, 1, inv, &objectCount, rawTarget)...)
		}
	}

	subscriptions, err := listSubscriptions(opCtx, c.subscriptions)
	if err != nil {
		collectionErrors = append(collectionErrors, model.CollectionError{Code: "azure_partial", Message: rawTarget + ": list subscriptions: " + err.Error(), Retryable: retryable(err)})
	} else {
		enrich(inv, subscriptions)
	}

	return inv, collectionErrors
}

// walk maps one node of the recursive management-group tree returned by
// the Get(..., Recurse: true) call — either a nested management group or a
// subscription — and recurses into its own children up to
// maxManagementGroupDepth. Unlike AWS's per-parent listing walk, Azure
// returns the whole hierarchy in a single call, so there is no per-node
// partial-failure case here: a failure fetching the tree at all is
// reported by discoverTarget as a single required-call error.
func walk(node *armmanagementgroups.ManagementGroupChildInfo, parentID string, depth int, inv *Inventory, objectCount *int, rawTarget string) []model.CollectionError {
	if node == nil || *objectCount >= maxObjects {
		if *objectCount >= maxObjects {
			return []model.CollectionError{{Code: "azure_operation", Message: fmt.Sprintf("%s: tenant inventory exceeded %d objects", rawTarget, maxObjects)}}
		}
		return nil
	}
	var errs []model.CollectionError
	armID := valueOfString(node.ID)
	switch {
	case node.Type != nil && *node.Type == armmanagementgroups.ManagementGroupChildTypeSubscriptions:
		inv.Subscriptions = append(inv.Subscriptions, SubscriptionInfo{
			ARMID: armID, ShortID: valueOfString(node.Name), DisplayName: valueOfString(node.DisplayName), TenantID: inv.Tenant.ID, ParentID: parentID,
		})
		*objectCount++
	default:
		inv.ManagementGroups = append(inv.ManagementGroups, ManagementGroupInfo{
			ARMID: armID, ShortID: valueOfString(node.Name), DisplayName: valueOfString(node.DisplayName), ParentID: parentID,
		})
		*objectCount++
		if depth < maxManagementGroupDepth {
			for _, child := range node.Children {
				errs = append(errs, walk(child, armID, depth+1, inv, objectCount, rawTarget)...)
			}
		}
	}
	return errs
}

func findTenant(ctx context.Context, tenants *armsubscriptions.TenantsClient, tenantID string) (TenantInfo, error) {
	pager := tenants.NewListPager(nil)
	seen := 0
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return TenantInfo{}, err
		}
		for _, t := range page.Value {
			seen++
			if seen > maxObjects {
				return TenantInfo{}, fmt.Errorf("tenant listing exceeded %d objects", maxObjects)
			}
			if valueOfString(t.TenantID) == tenantID {
				armID := valueOfString(t.ID)
				if armID == "" {
					armID = "/tenants/" + tenantID
				}
				return TenantInfo{ID: armID, DisplayName: valueOfString(t.DisplayName), DefaultDomain: valueOfString(t.DefaultDomain)}, nil
			}
		}
	}
	// The configured tenant did not appear in the caller's own tenant list
	// (a service principal is not always granted Tenants-list access even
	// when it can list management groups and subscriptions); fall back to
	// the well-formed ARM tenant ID so the hierarchy still has a root to
	// attach to, with no display attributes.
	return TenantInfo{ID: "/tenants/" + tenantID}, nil
}

func listSubscriptions(ctx context.Context, subscriptions *armsubscriptions.Client) ([]*armsubscriptions.Subscription, error) {
	var out []*armsubscriptions.Subscription
	pager := subscriptions.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
		if len(out) > maxObjects {
			return nil, fmt.Errorf("subscription listing exceeded %d objects", maxObjects)
		}
	}
	return out, nil
}

func (p Plugin) dial(rawTarget string) (*client, error) {
	target, err := validateTarget(rawTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	authority, err := validateTarget(p.Config.AuthorityURL, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	armEndpoint := target.String()
	cloudConfig := cloud.Configuration{
		ActiveDirectoryAuthorityHost: authority.String(),
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			cloud.ResourceManager: {Audience: armEndpoint, Endpoint: armEndpoint},
		},
	}
	clientOptions := azcore.ClientOptions{Cloud: cloudConfig}
	if p.Config.LabMode {
		// Topo Lab's Azure fixture presents a self-signed certificate;
		// production targets always verify the presented certificate.
		clientOptions.Transport = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // Topo Lab only, loopback-restricted by validateTarget above
	}
	cred, err := azidentity.NewClientSecretCredential(p.Config.TenantID, p.Config.ClientID, p.Config.ClientSecret, &azidentity.ClientSecretCredentialOptions{
		ClientOptions:            clientOptions,
		DisableInstanceDiscovery: p.Config.LabMode,
	})
	if err != nil {
		return nil, err
	}
	armOptions := &arm.ClientOptions{ClientOptions: clientOptions, DisableRPRegistration: true}
	mgClient, err := armmanagementgroups.NewClient(cred, armOptions)
	if err != nil {
		return nil, err
	}
	subClient, err := armsubscriptions.NewClient(cred, armOptions)
	if err != nil {
		return nil, err
	}
	tenantsClient, err := armsubscriptions.NewTenantsClient(cred, armOptions)
	if err != nil {
		return nil, err
	}
	return &client{managementGroups: mgClient, subscriptions: subClient, tenants: tenantsClient}, nil
}

func (p Plugin) operationTimeout() time.Duration {
	if p.Config.OperationTimeout > 0 {
		return p.Config.OperationTimeout
	}
	return 30 * time.Second
}

func validateTarget(raw string, labMode bool) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errors.New("Azure endpoint target exceeds 2048 bytes")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Azure endpoint target %q", raw)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("Azure endpoint target %q must not contain credentials", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Azure endpoint target %q must not contain a query or fragment", raw)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("Azure endpoint target %q must use HTTPS", raw)
	}
	if labMode {
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("Topo Lab Azure target %q must be loopback", raw)
		}
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

func valueOfString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func displayName(props *armmanagementgroups.ManagementGroupProperties) string {
	if props == nil {
		return ""
	}
	return valueOfString(props.DisplayName)
}

func observationID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("obs-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
