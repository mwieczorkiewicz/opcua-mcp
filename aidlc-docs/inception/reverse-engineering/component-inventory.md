# Component Inventory

## Application Packages
- `cmd/opcua-mcp` — process entrypoint
- `internal/mcp` — MCP server, tool/resource handlers, transport, shutdown
- `internal/opcua` — OPC-UA client wrapper + background discovery/search service

## Infrastructure Packages
- None. No CDK/Terraform/CloudFormation. `Dockerfile` provides a multi-stage
  container build (Chainguard Go builder → `scratch` runtime); no orchestration
  config (Kubernetes manifests, etc.) exists in-repo — `docker-compose.yml`
  examples are documented in `README.md` only, not checked in as files.

## Shared Packages
- `internal/config` — configuration models + parsing/validation
- `internal/logger` — structured logging utility

## Test Packages
- No separate test packages — tests live alongside the code they cover
  (`*_test.go` in each package), Go convention. No dedicated integration/load
  test packages exist; a couple of tests do real (non-mocked) I/O against
  local resources (`internal/mcp/server_test.go`'s HTTP listener test) but
  none require a live OPC-UA server.

## Total Count
- **Total Packages**: 5 (`cmd`, `internal/config`, `internal/logger`,
  `internal/mcp`, `internal/opcua`)
- **Application**: 3 (`cmd`, `internal/mcp`, `internal/opcua`)
- **Infrastructure**: 0
- **Shared**: 2 (`internal/config`, `internal/logger`)
- **Test**: 0 (co-located, not separate packages)
