package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Protect the reviewed publication condition itself, not merely a separate Go
// model of it. actionlint also checks the actual GitHub expression syntax.
func TestWorkflowRequiresEverySelectedSigner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	_, publish, ok := strings.Cut(workflow, "\n  publish:\n")
	if !ok {
		t.Fatal("missing publisher")
	}
	_, condition, ok := strings.Cut(publish, "    if: >-\n")
	if !ok {
		t.Fatal("publisher must explicitly handle a skipped Windows job")
	}
	condition, _, _ = strings.Cut(condition, "    permissions:")
	got := strings.Join(strings.Fields(condition), " ")
	want := "${{ !cancelled() && needs.build-packages.result == 'success' && needs.native-signing.result == 'success' && ((needs.build-packages.outputs.require_apple_signing == 'true' && needs.macos-signing.result == 'success' && needs.macos-homebrew.result == 'skipped') || (needs.build-packages.outputs.require_apple_signing == 'false' && needs.macos-signing.result == 'skipped' && needs.macos-homebrew.result == 'success')) && ((needs.build-packages.outputs.include_windows == 'true' && needs.windows-msi.result == 'success') || (needs.build-packages.outputs.include_windows == 'false' && needs.windows-msi.result == 'skipped')) }}"
	if got != want {
		t.Fatalf("publication condition changed; review signer failure/skip semantics:\n%s", got)
	}
	for _, required := range []string{
		"RELEASE_PROFILE: linux-homebrew-beta",
		"  macos-signing:\n    if: needs.build-packages.outputs.require_apple_signing == 'true'",
		"  macos-homebrew:\n    if: needs.build-packages.outputs.require_apple_signing == 'false'",
		"- name: Download signed and notarized macOS archives\n        if: needs.build-packages.outputs.require_apple_signing == 'true'",
		"  windows-msi:\n    if: needs.build-packages.outputs.include_windows == 'true'",
		"- name: Download signed MSI artifacts\n        if: needs.build-packages.outputs.include_windows == 'true'",
		"go run ./internal/releasetool -mode policy -profile \"$RELEASE_PROFILE\" -version \"$version\"",
		"jq -e '.status == \"Accepted\"'",
		"go run ./internal/releasetool -mode refresh-metadata -out dist",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("workflow missing %q", required)
		}
	}
}

func TestHomebrewPolicyAndNoSecurityBypass(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "promote.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	verify := strings.Index(workflow, "cosign verify-blob")
	policy := strings.Index(workflow, "id: policy")
	if verify < 0 || policy <= verify {
		t.Fatal("policy must follow manifest authentication")
	}
	for _, required := range []string{
		"require_apple_signing: ${{ steps.policy.outputs.require_apple_signing }}",
		"REQUIRE_APPLE_SIGNING: ${{ needs.prepare-and-test.outputs.require_apple_signing }}",
		`if [[ "$REQUIRE_APPLE_SIGNING" == true ]]; then`,
		`elif [[ "$REQUIRE_APPLE_SIGNING" == false ]]; then`,
		"missing or invalid authenticated Apple signing policy",
		"runner: [macos-15, macos-15-intel]",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("missing %q", required)
		}
	}
	for _, path := range []string{".github/workflows/promote.yml", ".github/workflows/release.yml", "scripts/test-homebrew-beta.sh"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, bypass := range []string{"--no-quarantine", "xattr -d", "xattr -c", "--master-disable", "--global-disable", "HOMEBREW_NO_VERIFY_ATTESTATIONS"} {
			if strings.Contains(string(data), bypass) {
				t.Fatalf("security bypass %q in %s", bypass, path)
			}
		}
	}
}
