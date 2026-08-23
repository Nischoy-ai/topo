.PHONY: test build run smoke lab-test release-snapshot package-snapshot security-review

test:
	go test ./...

build:
	mkdir -p bin
	go build -trimpath -o bin/topo ./cmd/topo

run:
	go run ./cmd/topo serve

smoke:
	go run ./cmd/topo discover local

lab-test: build
	go test -run TestDiscoverFiveHundredHostsAndIdempotentResolution ./pkg/lab

release-snapshot:
	scripts/build-release.sh v0.0.0-dev dev dist

package-snapshot:
	scripts/build-release.sh v0.0.0-dev dev dist-raw
	scripts/build-packages.sh v0.0.0-dev dist-raw dist

security-review:
	scripts/security-review-checks.sh
