# Technology Stack

## Programming Languages
- Go - 1.25.0 (module `go` directive) - entire codebase

## Frameworks / Libraries
- `github.com/gopcua/opcua` v0.9.0 - OPC-UA client protocol implementation
- `github.com/mark3labs/mcp-go` v0.39.1 - MCP protocol implementation (tools, resources, stdio + streamable-HTTP transports)
- `github.com/blevesearch/bleve/v2` v2.5.3 - embedded full-text search index
- `github.com/caarlos0/env/v11` v11.4.1 - struct-tag env-var parsing
- `go.etcd.io/bbolt` v1.4.0 - embedded KV store (currently indirect, via bleve; not yet used directly by this project's own code)
- Go standard library `log/slog` - structured logging (no third-party logging framework)

## Infrastructure
- None (no cloud services, no managed datastores). The only "infrastructure"
  is the local filesystem (Bleve index at `SEARCH_INDEX_PATH`) and the
  external OPC-UA server this project connects to (operator-provided, out
  of this repo's scope).

## Build Tools
- Go toolchain (`go build`, `go vet`, `go mod`) - primary
- `Makefile` - wraps common targets (`deps`, `fmt`, `lint`, `test`, `build`,
  `security-scan`, `vuln-check`, plus dev-environment helpers for running a
  local Microsoft OPC UA test server via Docker)
- Docker - multi-stage build (`cgr.dev/chainguard/go` builder → `scratch` runtime)
- `golangci-lint`/`staticcheck`/`gosec`/`govulncheck` - referenced by the
  Makefile but not reliably available in every dev environment (silently
  no-op locally if missing; `go vet`/`gofmt`/`go test` are the reliable
  local baseline)

## Testing Tools
- Go standard library `testing` package only - no third-party test framework
  (no testify, no ginkgo). Table-driven test style throughout.
- `go test -race` - used routinely for concurrency-sensitive code
  (`Client`'s connection state, `DiscoveryService`'s cache)
