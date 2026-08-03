# Tech Stack Decisions - Unit 3: Read-Through Caching & MCP Integration

## `github.com/testcontainers/testcontainers-go` (new, test-only dependency - pre-approved in `requirements.md` Q7)
- **Decision**: add as a direct dependency scoped to test files only
  (`go.mod`'s regular `require` block; nothing in non-test application code
  imports it).
- **Rationale**: real end-to-end verification of subscribe/reconnect/cache
  behavior against an actual OPC-UA server can't be done through the
  mocked `opcuaClient`/`subscribingClient` seams alone - those seams
  verify this project's own logic assuming gopcua behaves as documented;
  the integration suite verifies the assumption itself, against gopcua's
  real wire behavior and a real server's real responses.
- **Version**: latest stable at implementation time (checked via
  `go list -m -versions` during Code Generation, pinned in `go.sum` like
  every other dependency - SECURITY-10).
- **License**: MIT.

## Microsoft OPC-UA test server image (reused pin, not a new decision)
`mcr.microsoft.com/iot/opc-ua-test-server:2.8` - already pinned in this
repo's `docker-compose.yml` (added by concurrent work on the dev/test
Docker Compose stack, confirmed present in the working tree at the time
Unit 3 NFR Requirements was written). The integration suite's
`testcontainers-go` container request reuses this exact tag rather than
introducing a second reference to the same image with a potentially
different (drifting) tag.

## No other new dependencies
`CachingClient` needs nothing beyond `internal/store` (Unit 1),
`internal/opcua.Client` (existing), `internal/config` (existing), and Go
standard library (`context`, `time`). The 3 new MCP tools reuse
`mark3labs/mcp-go` (existing) and `opcua.ParseNodeIDs` (existing). `rapid`
(Unit 1) is reused for the new TTL-invariant property test (NFR-3.2) - no
second PBT framework.
