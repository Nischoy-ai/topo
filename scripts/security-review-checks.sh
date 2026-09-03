#!/bin/sh
set -eu

expected_go=go1.26.8
toolchain=go1.26.8
govulncheck=golang.org/x/vuln/cmd/govulncheck@v1.7.0

actual_go=$(GOTOOLCHAIN="$toolchain" go env GOVERSION)
if [ "$actual_go" != "$expected_go" ]; then
	echo "security review requires exactly $expected_go, found $actual_go" >&2
	exit 1
fi

review_work=$(mktemp -d "${TMPDIR:-/tmp}/topo-security-review.XXXXXX")
trap 'rm -rf "$review_work"' EXIT HUP INT TERM
gofmt_bin=$(GOTOOLCHAIN="$toolchain" go env GOROOT)/bin/gofmt

format_files=$("$gofmt_bin" -l .)
if [ -n "$format_files" ]; then
	echo "gofmt required for:" >&2
	echo "$format_files" >&2
	exit 1
fi

git diff --check
./scripts/check-workflow-interpolation.sh
GOTOOLCHAIN="$toolchain" go mod verify
GOTOOLCHAIN="$toolchain" go vet ./...
GOTOOLCHAIN="$toolchain" go run "$govulncheck" ./...
GOTOOLCHAIN="$toolchain" go test -race -coverprofile="$review_work/coverage.out" ./...
GOTOOLCHAIN="$toolchain" go build -trimpath -o "$review_work/topo" ./cmd/topo
GOTOOLCHAIN="$toolchain" GOOS=windows GOARCH=amd64 go vet ./...
GOTOOLCHAIN="$toolchain" GOOS=windows GOARCH=amd64 go build -trimpath \
	-o "$review_work/topo.exe" ./cmd/topo

echo "security review checks passed with $actual_go and govulncheck v1.7.0"
