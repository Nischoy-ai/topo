# Contributing

Run `gofmt -w .`, `go vet ./...`, and `go test -race ./...` before submitting a change. New plugins must include capability metadata, configuration validation, context cancellation, fixture-based tests, an explicit permissions list, and documentation of every command or API they use.

Schema changes require an architecture note explaining compatibility and updated JSON Schema, Protobuf, Go types, fixtures, and publisher contract tests. Never use an IP address as the sole long-lived identity for a device.

Topo is a standalone public repository. Contributions must not introduce dependencies on Nischoy's private repositories, internal services, customer data, or proprietary build infrastructure.
