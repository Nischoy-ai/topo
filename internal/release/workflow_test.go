package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release package job exercises this image before promotion can run. Keep
// promotion on that same immutable image rather than an independently copied pin.
func TestPromotionUsesReleaseFedoraImage(t *testing.T) {
	root := filepath.Join("..", "..")
	pin := regexp.MustCompile(`fedora@sha256:[a-f0-9]{64}\b`)
	var releaseImage string
	for _, path := range []string{"scripts/test-linux-packages.sh", ".github/workflows/promote.yml"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		images := pin.FindAllString(string(data), -1)
		if len(images) != 1 {
			t.Fatalf("%s must contain exactly one digest-pinned Fedora image, found %d", path, len(images))
		}
		if releaseImage == "" {
			releaseImage = images[0]
		} else if images[0] != releaseImage {
			t.Fatalf("promotion Fedora image %s differs from release-tested image %s", images[0], releaseImage)
		}
	}
}

func TestPromotionRPMLifecycleCannotBeSwallowedByHeredoc(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "promote.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, rpm, ok := strings.Cut(string(data), "- name: Exercise clean-machine RPM install and stable upgrade\n")
	if !ok {
		t.Fatal("missing RPM lifecycle gate")
	}
	rpm, _, _ = strings.Cut(rpm, "- name: Retain signed promotion transaction")
	// YAML indentation survives inside bash -euc's quoted argument. An indented
	// heredoc terminator consumes the following tests while bash still exits zero.
	if strings.Contains(rpm, "<<") {
		t.Fatal("RPM gate must not embed a heredoc inside the indented shell argument")
	}
	for _, required := range []string{
		`printf "%s\n"`, `"gpgcheck=1"`, `"repo_gpgcheck=1"`,
		`"baseurl=file:///repo/rpm/$CHANNEL/\$basearch"`,
		`dnf install -y topo`, `test "$(topo version)" = "$VERSION"`,
		`dnf remove -y topo`, `test ! -e /usr/bin/topo`,
		`echo "RPM repository lifecycle passed"`,
	} {
		if !strings.Contains(rpm, required) {
			t.Fatalf("RPM lifecycle gate missing %q", required)
		}
	}
}

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
	for _, path := range []string{".github/workflows/promote.yml", ".github/workflows/release.yml", "scripts/test-homebrew-beta.sh", "scripts/test-homebrew-promotion.sh"} {
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

func TestPublicHomebrewFormulaAuditInCI(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, job, ok := strings.Cut(string(data), "  homebrew-beta:\n")
	if !ok {
		t.Fatal("missing Homebrew matrix")
	}
	job, _, _ = strings.Cut(job, "  windows-package:\n")
	for _, required := range []string{"runner: [macos-15, macos-15-intel]", "bash scripts/test-homebrew-promotion.sh"} {
		if !strings.Contains(job, required) {
			t.Fatalf("public formula audit must run on both architectures: missing %q", required)
		}
	}
	data, err = os.ReadFile(filepath.Join(root, "scripts/test-homebrew-promotion.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		"version=v0.1.0-beta.1",
		"manifest_sha256=2da670111c37f7ad3249f790d9e1ba97cc700e9a7e38729fbc986e0251ab55ca",
		`"${RUNNER_ENVIRONMENT:-}" != github-hosted`,
		`brew audit --strict --online "$formula"`, `brew install --formula "$formula"`,
		`brew test "$formula"`, `test "$(topo version)" = "$version"`, `brew uninstall --formula "$formula"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing public formula gate %q", required)
		}
	}
	verify := strings.Index(script, `shasum -a 256 --check SHA256SUMS`)
	render := strings.Index(script, "go run ./internal/distributiontool")
	if verify < 0 || render <= verify {
		t.Fatal("fixture inputs must be verified before formula rendering")
	}
}
