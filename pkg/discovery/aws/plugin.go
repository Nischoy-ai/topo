// Package aws discovers an AWS Organization's own account structure —
// which accounts exist and how they are grouped into organizational units
// under the organization's roots — over the real AWS Organizations API,
// using only read-only Describe/List calls. It never issues a create,
// invite, move, tag, or policy operation against any organization object.
package aws

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
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

// maxObjects bounds the total number of roots, organizational units, and
// accounts collected for a single target, matching the Kubernetes node/pod
// cap, the VMware managed-object cap, and the SNMP interface-table walk
// cap. This slice does not implement chunked pagination beyond that single
// bound — an organization with more objects than the cap is reported as a
// collection error rather than silently truncated.
const maxObjects = 100000

// maxOUDepth bounds organizational-unit recursion to 5 levels below each
// root. This is not an arbitrary safety margin: AWS Organizations itself
// enforces a 5-level OU nesting limit, so a conformant API can never
// require deeper recursion. The bound still exists in code — rather than
// trusting the API to honor its own documented limit — for the same
// defense-in-depth reason every other Topo plugin bounds its walks: a
// misbehaving or hostile endpoint must not be able to force unbounded
// recursion.
const maxOUDepth = 5

type Config struct {
	AccessKeyID     string
	SecretAccessKey string
	// SessionToken is optional: it is required only when AccessKeyID and
	// SecretAccessKey are temporary STS credentials (for example, from
	// assuming a read-only cross-account organization role), not for
	// long-lived IAM user access keys.
	SessionToken string
	// Region is required and never defaulted or autodetected — AWS
	// Organizations is only reachable from specific regional endpoints
	// depending on partition (us-east-1 for the standard aws partition),
	// and Topo never guesses a partition's home region on a caller's
	// behalf, matching the project's explicit-target invariant.
	Region string
	// LabMode permits an HTTP, loopback-only target instead of the normal
	// HTTPS requirement, restricted to Topo Lab.
	LabMode     bool
	Concurrency int
	// OperationTimeout bounds the full Describe/List call sequence for one
	// target, including pagination. There is no separate connect phase to
	// give its own timeout, the same as Kubernetes's bearer-token REST
	// client.
	OperationTimeout time.Duration
}

type Plugin struct{ Config Config }

func (p Plugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{
		Name:       "aws-organizations",
		Version:    "0.1.0",
		AssetTypes: []model.AssetType{model.AssetCloudResource},
		RequiredPermissions: []string{
			"read-only AWS Organizations IAM permissions to describe the organization and list its roots, organizational units, and accounts (organizations:DescribeOrganization, organizations:ListRoots, organizations:ListOrganizationalUnitsForParent, organizations:ListAccountsForParent — the AWS managed AWSOrganizationsReadOnlyAccess policy is sufficient; no write, invite, move, tag, or policy action is ever used)",
		},
	}
}

func (p Plugin) ValidateConfiguration(_ context.Context, r discovery.Request) error {
	if len(r.Targets) == 0 {
		return errors.New("at least one AWS Organizations API endpoint target is required")
	}
	if p.Config.Concurrency < 0 || p.Config.Concurrency > 1024 {
		return errors.New("AWS Organizations concurrency must be between 0 (default) and 1024")
	}
	if p.Config.OperationTimeout < 0 {
		return errors.New("AWS Organizations operation timeout cannot be negative")
	}
	for key := range r.Options {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "credential") {
			return fmt.Errorf("AWS secrets are not accepted in request option %q", key)
		}
	}
	if p.Config.AccessKeyID == "" || p.Config.SecretAccessKey == "" {
		return errors.New("AWS access key ID and secret access key are required")
	}
	if len(p.Config.AccessKeyID) > 512 {
		return errors.New("AWS access key ID exceeds 512 bytes")
	}
	if len(p.Config.SecretAccessKey) > 4096 {
		return errors.New("AWS secret access key exceeds 4096 bytes")
	}
	if len(p.Config.SessionToken) > 8192 {
		return errors.New("AWS session token exceeds 8192 bytes")
	}
	if strings.ContainsAny(p.Config.AccessKeyID, "\x00\r\n") {
		return errors.New("AWS access key ID contains a control character")
	}
	if strings.ContainsAny(p.Config.SessionToken, "\x00\r\n") {
		return errors.New("AWS session token contains a control character")
	}
	if p.Config.Region == "" {
		return errors.New("AWS region is required")
	}
	if len(p.Config.Region) > 64 {
		return errors.New("AWS region exceeds 64 bytes")
	}
	if strings.ContainsAny(p.Config.Region, "\x00\r\n") {
		return errors.New("AWS region contains a control character")
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
	client, err := p.dial(r.Targets[0])
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, p.operationTimeout())
	defer cancel()
	_, err = client.DescribeOrganization(opCtx, &organizations.DescribeOrganizationInput{})
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
		CollectorID:   valueOr(r.CollectorID, "aws-relay"),
		Plugin:        "aws-organizations",
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
	client, err := p.dial(rawTarget)
	if err != nil {
		return nil, []model.CollectionError{{Code: "aws_organizations_connect", Message: rawTarget + ": " + err.Error(), Retryable: retryable(err)}}
	}

	opCtx, cancel := context.WithTimeout(ctx, p.operationTimeout())
	defer cancel()

	descOut, err := client.DescribeOrganization(opCtx, &organizations.DescribeOrganizationInput{})
	if err != nil {
		return nil, []model.CollectionError{{Code: "aws_organizations_operation", Message: rawTarget + ": describe organization: " + err.Error(), Retryable: retryable(err)}}
	}
	if descOut.Organization == nil || descOut.Organization.Id == nil {
		return nil, []model.CollectionError{{Code: "aws_organizations_operation", Message: rawTarget + ": describe organization returned no organization"}}
	}

	roots, err := listAllRoots(opCtx, client)
	if err != nil {
		return nil, []model.CollectionError{{Code: "aws_organizations_operation", Message: rawTarget + ": list roots: " + err.Error(), Retryable: retryable(err)}}
	}

	inv := &Inventory{Organization: OrganizationInfo{
		ID:                     awssdk.ToString(descOut.Organization.Id),
		ARN:                    awssdk.ToString(descOut.Organization.Arn),
		FeatureSet:             string(descOut.Organization.FeatureSet),
		ManagementAccountID:    awssdk.ToString(descOut.Organization.MasterAccountId),
		ManagementAccountARN:   awssdk.ToString(descOut.Organization.MasterAccountArn),
		ManagementAccountEmail: awssdk.ToString(descOut.Organization.MasterAccountEmail),
	}}

	var collectionErrors []model.CollectionError
	objectCount := 0
	for _, root := range roots {
		rootID := awssdk.ToString(root.Id)
		if objectCount >= maxObjects {
			collectionErrors = append(collectionErrors, model.CollectionError{Code: "aws_organizations_operation", Message: fmt.Sprintf("%s: organization inventory exceeded %d objects", rawTarget, maxObjects)})
			break
		}
		inv.Roots = append(inv.Roots, RootInfo{ID: rootID, Name: awssdk.ToString(root.Name), ARN: awssdk.ToString(root.Arn)})
		objectCount++
		errs := p.walk(opCtx, client, rawTarget, rootID, 0, inv, &objectCount)
		collectionErrors = append(collectionErrors, errs...)
	}

	return inv, collectionErrors
}

// walk lists the organizational units and accounts directly under parentID
// (a root or OU ID), then recurses into each child OU up to maxOUDepth. A
// failure listing one parent's children is reported as a partial-collection
// error and that subtree is skipped; it does not abort discovery of the
// target's other roots or sibling OUs.
func (p Plugin) walk(ctx context.Context, client *organizations.Client, rawTarget, parentID string, depth int, inv *Inventory, objectCount *int) []model.CollectionError {
	var errs []model.CollectionError

	accounts, err := listAllAccountsForParent(ctx, client, parentID)
	if err != nil {
		errs = append(errs, model.CollectionError{Code: "aws_organizations_partial", Message: fmt.Sprintf("%s: list accounts for %s: %v", rawTarget, parentID, err), Retryable: retryable(err)})
	} else {
		for _, account := range accounts {
			if *objectCount >= maxObjects {
				errs = append(errs, model.CollectionError{Code: "aws_organizations_operation", Message: fmt.Sprintf("%s: organization inventory exceeded %d objects", rawTarget, maxObjects)})
				return errs
			}
			inv.Accounts = append(inv.Accounts, AccountInfo{
				ID:              awssdk.ToString(account.Id),
				Name:            awssdk.ToString(account.Name),
				ARN:             awssdk.ToString(account.Arn),
				Email:           awssdk.ToString(account.Email),
				State:           string(account.State),
				JoinedMethod:    string(account.JoinedMethod),
				JoinedTimestamp: account.JoinedTimestamp,
				ParentID:        parentID,
			})
			*objectCount++
		}
	}

	if depth >= maxOUDepth {
		return errs
	}
	ous, err := listAllOUsForParent(ctx, client, parentID)
	if err != nil {
		errs = append(errs, model.CollectionError{Code: "aws_organizations_partial", Message: fmt.Sprintf("%s: list organizational units for %s: %v", rawTarget, parentID, err), Retryable: retryable(err)})
		return errs
	}
	for _, ou := range ous {
		if *objectCount >= maxObjects {
			errs = append(errs, model.CollectionError{Code: "aws_organizations_operation", Message: fmt.Sprintf("%s: organization inventory exceeded %d objects", rawTarget, maxObjects)})
			return errs
		}
		ouID := awssdk.ToString(ou.Id)
		inv.OUs = append(inv.OUs, OUInfo{ID: ouID, Name: awssdk.ToString(ou.Name), ARN: awssdk.ToString(ou.Arn), Path: awssdk.ToString(ou.Path), ParentID: parentID})
		*objectCount++
		errs = append(errs, p.walk(ctx, client, rawTarget, ouID, depth+1, inv, objectCount)...)
	}
	return errs
}

func listAllRoots(ctx context.Context, client *organizations.Client) ([]orgtypes.Root, error) {
	var out []orgtypes.Root
	var token *string
	for {
		resp, err := client.ListRoots(ctx, &organizations.ListRootsInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Roots...)
		if len(out) > maxObjects {
			return nil, fmt.Errorf("root listing exceeded %d objects", maxObjects)
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			return out, nil
		}
		token = resp.NextToken
	}
}

func listAllOUsForParent(ctx context.Context, client *organizations.Client, parentID string) ([]orgtypes.OrganizationalUnit, error) {
	var out []orgtypes.OrganizationalUnit
	var token *string
	for {
		resp, err := client.ListOrganizationalUnitsForParent(ctx, &organizations.ListOrganizationalUnitsForParentInput{ParentId: awssdk.String(parentID), NextToken: token})
		if err != nil {
			return nil, err
		}
		out = append(out, resp.OrganizationalUnits...)
		if len(out) > maxObjects {
			return nil, fmt.Errorf("organizational unit listing exceeded %d objects", maxObjects)
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			return out, nil
		}
		token = resp.NextToken
	}
}

func listAllAccountsForParent(ctx context.Context, client *organizations.Client, parentID string) ([]orgtypes.Account, error) {
	var out []orgtypes.Account
	var token *string
	for {
		resp, err := client.ListAccountsForParent(ctx, &organizations.ListAccountsForParentInput{ParentId: awssdk.String(parentID), NextToken: token})
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Accounts...)
		if len(out) > maxObjects {
			return nil, fmt.Errorf("account listing exceeded %d objects", maxObjects)
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			return out, nil
		}
		token = resp.NextToken
	}
}

func (p Plugin) dial(rawTarget string) (*organizations.Client, error) {
	target, err := validateTarget(rawTarget, p.Config.LabMode)
	if err != nil {
		return nil, err
	}
	endpoint := target.String()
	cfg := awssdk.Config{
		Region:      p.Config.Region,
		Credentials: credentials.NewStaticCredentialsProvider(p.Config.AccessKeyID, p.Config.SecretAccessKey, p.Config.SessionToken),
	}
	return organizations.NewFromConfig(cfg, func(o *organizations.Options) {
		o.BaseEndpoint = awssdk.String(endpoint)
	}), nil
}

func (p Plugin) operationTimeout() time.Duration {
	if p.Config.OperationTimeout > 0 {
		return p.Config.OperationTimeout
	}
	return 30 * time.Second
}

func validateTarget(raw string, labMode bool) (*url.URL, error) {
	if len(raw) > 2048 {
		return nil, errors.New("AWS Organizations API endpoint target exceeds 2048 bytes")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid AWS Organizations API endpoint target %q", raw)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("AWS Organizations API endpoint target %q must not contain credentials", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("AWS Organizations API endpoint target %q must not contain a query or fragment", raw)
	}
	if labMode {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("Topo Lab AWS Organizations target %q must use HTTP or HTTPS", raw)
		}
		hostname := parsed.Hostname()
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("Topo Lab AWS Organizations target %q must be loopback", raw)
		}
	} else if parsed.Scheme != "https" {
		return nil, fmt.Errorf("AWS Organizations API endpoint target %q must use HTTPS", raw)
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
