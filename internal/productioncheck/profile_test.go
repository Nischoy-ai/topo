package productioncheck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLinuxMacOSReadiness(t *testing.T) {
	for _, missing := range []string{"", "APPLE_NOTARY_PRIVATE_KEY", "RPM_SIGNING_PRIVATE_KEY"} {
		responses := readyResponses(t)
		var values []map[string]string
		for _, name := range nativeSecretNames {
			if strings.HasPrefix(name, "AZURE_") || strings.HasPrefix(name, "ARTIFACT_SIGNING_") || name == missing {
				continue
			}
			values = append(values, map[string]string{"name": name})
		}
		data, err := json.Marshal(map[string]any{"total_count": len(values), "secrets": values})
		if err != nil {
			t.Fatal(err)
		}
		responses["repos/Nischoy-ai/topo/environments/native-package-signing/secrets"] = string(data)
		options := Options{Owner: "Nischoy-ai", Repository: "topo", Profile: "linux-macos-beta"}
		report, err := Run(context.Background(), fakeAPI{responses: responses}, options)
		if err != nil {
			t.Fatal(err)
		}
		if report.Ready != (missing == "") || report.Profile != options.Profile {
			t.Fatalf("missing %q: %+v", missing, report)
		}
		options.Profile = "all"
		report, err = Run(context.Background(), fakeAPI{responses: responses}, options)
		if err != nil || report.Ready {
			t.Fatalf("full profile lost Windows requirement: %+v %v", report, err)
		}
	}
	if _, err := Run(context.Background(), fakeAPI{}, Options{Profile: "typo"}); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestHomebrewReadinessKeepsRemainingGates(t *testing.T) {
	for _, missing := range []string{"", "RPM_SIGNING_PRIVATE_KEY", "RPM_SIGNING_FINGERPRINT", "DISTRIBUTION_GITHUB_TOKEN", "REPOSITORY_SIGNING_PRIVATE_KEY"} {
		t.Run(missing, func(t *testing.T) {
			responses := readyResponses(t)
			for environment, names := range map[string][]string{"native-package-signing": nativeSecretNames, "distribution-beta": betaSecretNames} {
				var values []map[string]string
				for _, name := range names {
					if strings.HasPrefix(name, "APPLE_") || strings.HasPrefix(name, "AZURE_") || strings.HasPrefix(name, "ARTIFACT_SIGNING_") || name == missing {
						continue
					}
					values = append(values, map[string]string{"name": name})
				}
				data, err := json.Marshal(map[string]any{"total_count": len(values), "secrets": values})
				if err != nil {
					t.Fatal(err)
				}
				responses["repos/Nischoy-ai/topo/environments/"+environment+"/secrets"] = string(data)
			}
			options := Options{Owner: "Nischoy-ai", Repository: "topo", Profile: "linux-homebrew-beta"}
			report, err := Run(context.Background(), fakeAPI{responses: responses}, options)
			if err != nil || report.Ready != (missing == "") {
				t.Fatalf("%+v %v", report, err)
			}
			options.Profile = "linux-macos-beta"
			report, err = Run(context.Background(), fakeAPI{responses: responses}, options)
			if err != nil || report.Ready {
				t.Fatalf("signed beta lost Apple requirement: %+v %v", report, err)
			}
		})
	}
}
