.PHONY: test build run smoke lab-test

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
