package productioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeAPI struct {
	responses map[string]string
	failPath  string
}

func (f fakeAPI) Get(_ context.Context, path string) ([]byte, error) {
	if path == f.failPath {
		return nil, errors.New("secret-token-must-not-appear")
	}
	value, ok := f.responses[path]
	if !ok {
		return nil, errors.New("missing fixture")
	}
	return []byte(value), nil
}

func TestRunReady(t *testing.T) {
	report, err := Run(context.Background(), fakeAPI{responses: readyResponses(t)}, Options{Owner: "Nischoy-ai", Repository: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || len(report.Checks) != 9 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, check := range report.Checks {
		if check.Status != "pass" || check.Detail != "" {
			t.Fatalf("unexpected check: %+v", check)
		}
	}
}

func TestRunReportsMissingSecretsAndUnsafeEnvironment(t *testing.T) {
	responses := readyResponses(t)
	responses["repos/Nischoy-ai/topo/environments/native-package-signing"] = environmentJSON(t, "native-package-signing", true, 1)
	responses["repos/Nischoy-ai/topo/environments/distribution-beta/secrets"] = `{"total_count":0,"secrets":[]}`
	report, err := Run(context.Background(), fakeAPI{responses: responses}, Options{Owner: "Nischoy-ai", Repository: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready {
		t.Fatal("unsafe environment reported ready")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{"administrator protection-rule bypass", "at least two eligible reviewers", "DISTRIBUTION_GITHUB_TOKEN"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report omits %q: %s", expected, text)
		}
	}
}

func TestRunRedactsCommandFailure(t *testing.T) {
	const path = "repos/Nischoy-ai/topo-packages"
	report, err := Run(context.Background(), fakeAPI{responses: readyResponses(t), failPath: path}, Options{Owner: "Nischoy-ai", Repository: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-token") || !strings.Contains(string(encoded), "GitHub query failed") {
		t.Fatalf("unsafe failure report: %s", encoded)
	}
}

func TestRunRejectsMalformedAndTrailingJSON(t *testing.T) {
	for name, malformed := range map[string]string{
		"malformed": `{`,
		"trailing":  `{}` + "\n{}",
	} {
		t.Run(name, func(t *testing.T) {
			responses := readyResponses(t)
			responses["repos/Nischoy-ai/topo-packages"] = malformed
			report, err := Run(context.Background(), fakeAPI{responses: responses}, Options{Owner: "Nischoy-ai", Repository: "topo"})
			if err != nil {
				t.Fatal(err)
			}
			if report.Ready || report.Checks[0].Status != "fail" {
				t.Fatalf("malformed response reported ready: %+v", report)
			}
		})
	}
}

func TestRunRejectsOversizedResponse(t *testing.T) {
	responses := readyResponses(t)
	responses["repos/Nischoy-ai/topo-packages"] = strings.Repeat("x", maxResponseBytes+1)
	report, err := Run(context.Background(), fakeAPI{responses: responses}, Options{Owner: "Nischoy-ai", Repository: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || !strings.Contains(report.Checks[0].Detail, "exceeds 1 MiB") {
		t.Fatalf("oversized response reported ready: %+v", report)
	}
}

func TestRunRejectsDuplicateAndUnexpectedSecretNames(t *testing.T) {
	responses := readyResponses(t)
	responses["repos/Nischoy-ai/topo/environments/distribution-beta/secrets"] = `{"total_count":5,"secrets":[{"name":"DISTRIBUTION_GITHUB_TOKEN"},{"name":"DISTRIBUTION_GITHUB_TOKEN"},{"name":"REPOSITORY_SIGNING_FINGERPRINT"},{"name":"REPOSITORY_SIGNING_PRIVATE_KEY"},{"name":"UNREVIEWED_SECRET"}]}`
	report, err := Run(context.Background(), fakeAPI{responses: responses}, Options{Owner: "Nischoy-ai", Repository: "topo"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || !strings.Contains(report.Checks[8].Detail, "duplicate") {
		t.Fatalf("duplicate secret reported ready: %+v", report)
	}
}

func TestRunRejectsInvalidOptions(t *testing.T) {
	if _, err := Run(context.Background(), fakeAPI{}, Options{Owner: "../owner", Repository: "topo"}); err == nil {
		t.Fatal("unsafe owner accepted")
	}
}

func readyResponses(t *testing.T) map[string]string {
	t.Helper()
	responses := map[string]string{
		"repos/Nischoy-ai/topo-packages":       `{"full_name":"Nischoy-ai/topo-packages","visibility":"public","default_branch":"main","archived":false,"disabled":false}`,
		"repos/Nischoy-ai/homebrew-tap":        `{"full_name":"Nischoy-ai/homebrew-tap","visibility":"public","default_branch":"main","archived":false,"disabled":false}`,
		"repos/Nischoy-ai/topo-packages/pages": `{"html_url":"https://nischoy-ai.github.io/topo-packages/","build_type":"legacy","public":true,"https_enforced":true,"source":{"branch":"main","path":"/"}}`,
	}
	for _, environment := range []struct {
		name    string
		secrets []string
	}{
		{name: "native-package-signing", secrets: nativeSecretNames},
		{name: "distribution-beta", secrets: betaSecretNames},
	} {
		base := "repos/Nischoy-ai/topo/environments/" + environment.name
		responses[base] = environmentJSON(t, environment.name, false, 2)
		responses[base+"/deployment-branch-policies"] = `{"total_count":1,"branch_policies":[{"name":"main","type":"branch"}]}`
		values := make([]map[string]string, 0, len(environment.secrets))
		for _, name := range environment.secrets {
			values = append(values, map[string]string{"name": name})
		}
		contents, err := json.Marshal(map[string]any{"total_count": len(values), "secrets": values})
		if err != nil {
			t.Fatal(err)
		}
		responses[base+"/secrets"] = string(contents)
	}
	return responses
}

func environmentJSON(t *testing.T, name string, bypass bool, reviewers int) string {
	t.Helper()
	items := make([]map[string]any, reviewers)
	for index := range items {
		items[index] = map[string]any{"type": "User"}
	}
	contents, err := json.Marshal(map[string]any{
		"name":              name,
		"can_admins_bypass": bypass,
		"protection_rules": []any{
			map[string]any{"type": "required_reviewers", "prevent_self_review": true, "reviewers": items},
			map[string]any{"type": "branch_policy"},
		},
		"deployment_branch_policy": map[string]any{"protected_branches": false, "custom_branch_policies": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
