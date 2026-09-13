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
	want := "${{ !cancelled() && needs.build-packages.result == 'success' && needs.native-signing.result == 'success' && needs.macos-signing.result == 'success' && ((needs.build-packages.outputs.include_windows == 'true' && needs.windows-msi.result == 'success') || (needs.build-packages.outputs.include_windows == 'false' && needs.windows-msi.result == 'skipped')) }}"
	if got != want {
		t.Fatalf("publication condition changed; review signer failure/skip semantics:\n%s", got)
	}
	for _, required := range []string{
		"RELEASE_PROFILE: linux-macos-beta",
		"  windows-msi:\n    if: needs.build-packages.outputs.include_windows == 'true'",
		"- name: Download signed MSI artifacts\n        if: needs.build-packages.outputs.include_windows == 'true'",
		"go run ./internal/releasetool -mode profile -profile \"$RELEASE_PROFILE\" -version \"$version\"",
		"jq -e '.status == \"Accepted\"'",
		"go run ./internal/releasetool -mode refresh-metadata -out dist",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("workflow missing %q", required)
		}
	}
}
