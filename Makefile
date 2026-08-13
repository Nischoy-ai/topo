.PHONY: test build run smoke

test:
	go test ./...

build:
	mkdir -p bin
	go build -trimpath -o bin/topo ./cmd/topo

run:
	go run ./cmd/topo serve

smoke:
	go run ./cmd/topo discover local

