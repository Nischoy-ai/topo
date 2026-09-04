// Package productioncheck validates the external GitHub prerequisites for a
// Topo production distribution without reading secret values.
package productioncheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

const maxResponseBytes = 1 << 20

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

var nativeSecretNames = []string{
	"APPLE_DEVELOPER_ID_IDENTITY",
	"APPLE_DEVELOPER_ID_P12_BASE64",
	"APPLE_DEVELOPER_ID_P12_PASSWORD",
	"APPLE_NOTARY_ISSUER_ID",
	"APPLE_NOTARY_KEY_ID",
	"APPLE_NOTARY_PRIVATE_KEY",
	"RPM_SIGNING_FINGERPRINT",
	"RPM_SIGNING_PRIVATE_KEY",
	"WINDOWS_SIGNING_PFX_BASE64",
	"WINDOWS_SIGNING_PFX_PASSWORD",
}

var betaSecretNames = []string{
	"DISTRIBUTION_GITHUB_TOKEN",
	"REPOSITORY_SIGNING_FINGERPRINT",
	"REPOSITORY_SIGNING_PRIVATE_KEY",
}

var optionalBetaSecretNames = map[string]struct{}{
	"REPOSITORY_ADDITIONAL_PUBLIC_KEY":             {},
	"REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT": {},
}

// API returns one GitHub REST response. Implementations must not include
// command stderr in returned errors because it may contain authentication
// details.
type API interface {
	Get(context.Context, string) ([]byte, error)
}

// Options names the source repository and owning organization.
type Options struct {
	Owner      string
	Repository string
}

// Check is one secret-free preflight result.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report is the bounded, machine-readable preflight result.
type Report struct {
	SchemaVersion int     `json:"schema_version"`
	Ready         bool    `json:"ready"`
	Checks        []Check `json:"checks"`
}

// Run checks the first-beta repositories, Pages origin, protected
// environments, branch policies, and secret names. It never requests a secret
// value; GitHub's environment-secret listing endpoint returns names only.
func Run(ctx context.Context, api API, options Options) (Report, error) {
	if api == nil {
		return Report{}, errors.New("GitHub API is required")
	}
	if !slugPattern.MatchString(options.Owner) || !slugPattern.MatchString(options.Repository) {
		return Report{}, errors.New("owner and repository must be bounded GitHub slugs")
	}

	report := Report{SchemaVersion: 1, Ready: true}
	add := func(name string, err error) {
		check := Check{Name: name, Status: "pass"}
		if err != nil {
			check.Status = "fail"
			check.Detail = err.Error()
			report.Ready = false
		}
		report.Checks = append(report.Checks, check)
	}

	for _, repository := range []string{"topo-packages", "homebrew-tap"} {
		var value repositoryResponse
		err := getJSON(ctx, api, "repos/"+options.Owner+"/"+repository, &value)
		if err == nil {
			err = validateRepository(value, options.Owner+"/"+repository)
		}
		add("repository:"+repository, err)
	}

	var pages pagesResponse
	err := getJSON(ctx, api, "repos/"+options.Owner+"/topo-packages/pages", &pages)
	if err == nil {
		err = validatePages(pages, options.Owner)
	}
	add("pages:topo-packages", err)

	for _, environment := range []struct {
		name     string
		required []string
		optional map[string]struct{}
	}{
		{name: "native-package-signing", required: nativeSecretNames},
		{name: "distribution-beta", required: betaSecretNames, optional: optionalBetaSecretNames},
	} {
		base := "repos/" + options.Owner + "/" + options.Repository + "/environments/" + environment.name

		var policy environmentResponse
		err := getJSON(ctx, api, base, &policy)
		if err == nil {
			err = validateEnvironment(policy, environment.name)
		}
		add("environment:"+environment.name, err)

		var branches branchPoliciesResponse
		err = getJSON(ctx, api, base+"/deployment-branch-policies", &branches)
		if err == nil {
			err = validateBranches(branches)
		}
		add("branches:"+environment.name, err)

		var secrets secretsResponse
		err = getJSON(ctx, api, base+"/secrets", &secrets)
		if err == nil {
			err = validateSecrets(secrets, environment.required, environment.optional)
		}
		add("secrets:"+environment.name, err)
	}

	return report, nil
}

type repositoryResponse struct {
	FullName      string `json:"full_name"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Disabled      bool   `json:"disabled"`
}

type pagesResponse struct {
	HTMLURL       string `json:"html_url"`
	BuildType     string `json:"build_type"`
	Public        bool   `json:"public"`
	HTTPSEnforced bool   `json:"https_enforced"`
	Source        struct {
		Branch string `json:"branch"`
		Path   string `json:"path"`
	} `json:"source"`
}

type environmentResponse struct {
	Name            string `json:"name"`
	CanAdminsBypass bool   `json:"can_admins_bypass"`
	ProtectionRules []struct {
		Type              string            `json:"type"`
		PreventSelfReview bool              `json:"prevent_self_review"`
		Reviewers         []json.RawMessage `json:"reviewers"`
	} `json:"protection_rules"`
	DeploymentBranchPolicy *struct {
		ProtectedBranches    bool `json:"protected_branches"`
		CustomBranchPolicies bool `json:"custom_branch_policies"`
	} `json:"deployment_branch_policy"`
}

type branchPoliciesResponse struct {
	TotalCount     int `json:"total_count"`
	BranchPolicies []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"branch_policies"`
}

type secretsResponse struct {
	TotalCount int `json:"total_count"`
	Secrets    []struct {
		Name string `json:"name"`
	} `json:"secrets"`
}

func getJSON(ctx context.Context, api API, path string, destination any) error {
	contents, err := api.Get(ctx, path)
	if err != nil {
		return errors.New("GitHub query failed")
	}
	if len(contents) == 0 || len(contents) > maxResponseBytes {
		return errors.New("GitHub response is empty or exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("GitHub response is invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("GitHub response contains trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return errors.New("GitHub response contains invalid trailing data")
	}
	return nil
}

func validateRepository(value repositoryResponse, expected string) error {
	if value.FullName != expected || value.Visibility != "public" || value.DefaultBranch != "main" || value.Archived || value.Disabled {
		return errors.New("repository must be active, public, and use main as its default branch")
	}
	return nil
}

func validatePages(value pagesResponse, owner string) error {
	expectedURL := "https://" + strings.ToLower(owner) + ".github.io/topo-packages/"
	if value.HTMLURL != expectedURL || value.BuildType != "legacy" || !value.Public || !value.HTTPSEnforced || value.Source.Branch != "main" || value.Source.Path != "/" {
		return errors.New("Pages must be public HTTPS from main at the repository root")
	}
	return nil
}

func validateEnvironment(value environmentResponse, expected string) error {
	issues := make([]string, 0, 5)
	if value.Name != expected {
		issues = append(issues, "environment name does not match")
	}
	if value.CanAdminsBypass {
		issues = append(issues, "administrator protection-rule bypass must be disabled")
	}
	if value.DeploymentBranchPolicy == nil || value.DeploymentBranchPolicy.ProtectedBranches || !value.DeploymentBranchPolicy.CustomBranchPolicies {
		issues = append(issues, "environment must use custom deployment branch policies")
	}
	reviewerRules := 0
	branchRules := 0
	for _, rule := range value.ProtectionRules {
		switch rule.Type {
		case "required_reviewers":
			reviewerRules++
			if !rule.PreventSelfReview || len(rule.Reviewers) < 2 {
				issues = append(issues, "environment must prevent self-review and have at least two eligible reviewers")
			}
		case "branch_policy":
			branchRules++
		default:
			issues = append(issues, fmt.Sprintf("unexpected protection rule %q", rule.Type))
		}
	}
	if reviewerRules != 1 || branchRules != 1 {
		issues = append(issues, "environment must have one reviewer rule and one branch-policy rule")
	}
	if len(issues) > 0 {
		return errors.New(strings.Join(issues, "; "))
	}
	return nil
}

func validateBranches(value branchPoliciesResponse) error {
	if value.TotalCount != len(value.BranchPolicies) || value.TotalCount != 1 || value.BranchPolicies[0].Name != "main" || value.BranchPolicies[0].Type != "branch" {
		return errors.New("deployment policy must allow exactly the main branch")
	}
	return nil
}

func validateSecrets(value secretsResponse, required []string, optional map[string]struct{}) error {
	if value.TotalCount != len(value.Secrets) {
		return errors.New("secret count does not match the returned secret names")
	}
	seen := make(map[string]struct{}, len(value.Secrets))
	for _, secret := range value.Secrets {
		if !slugPattern.MatchString(secret.Name) {
			return errors.New("secret list contains an invalid name")
		}
		if _, duplicate := seen[secret.Name]; duplicate {
			return errors.New("secret list contains a duplicate name")
		}
		seen[secret.Name] = struct{}{}
	}

	missing := make([]string, 0)
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, name := range required {
		allowed[name] = struct{}{}
		if _, ok := seen[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range optional {
		allowed[name] = struct{}{}
	}
	unexpected := make([]string, 0)
	for name := range seen {
		if _, ok := allowed[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	if _, publicKey := seen["REPOSITORY_ADDITIONAL_PUBLIC_KEY"]; publicKey {
		if _, fingerprint := seen["REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT"]; !fingerprint {
			missing = append(missing, "REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT")
		}
	} else if _, fingerprint := seen["REPOSITORY_ADDITIONAL_PUBLIC_KEY_FINGERPRINT"]; fingerprint {
		missing = append(missing, "REPOSITORY_ADDITIONAL_PUBLIC_KEY")
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	if len(missing) > 0 || len(unexpected) > 0 {
		parts := make([]string, 0, 2)
		if len(missing) > 0 {
			parts = append(parts, "missing: "+strings.Join(missing, ", "))
		}
		if len(unexpected) > 0 {
			parts = append(parts, "unexpected: "+strings.Join(unexpected, ", "))
		}
		return errors.New(strings.Join(parts, "; "))
	}
	return nil
}
