# Code Quality Assessment

## Test Coverage
- **Overall**: 123 tests across 5 packages, all passing under `go test -race ./...`.
- **By package**: `internal/config` 96.3%, `internal/opcua` 43.8%, `internal/mcp` 31.4%, `internal/logger` 7.9%, `cmd` 0% (no test files — thin entrypoint, acceptable).
- **Unit Tests**: Good — table-driven throughout; concurrency-sensitive code
  (`opcua.Client`'s connection state, `DiscoveryService`'s cache) has
  dedicated `-race` coverage.
- **Integration Tests**: None against a live/simulated OPC-UA server — all
  OPC-UA interaction is mocked via the `opcuaClient` interface seam
  (`internal/opcua/mock_client_test.go`). This is a deliberate, documented
  tradeoff (fast, hermetic tests) rather than an oversight, but it means
  gopcua's actual wire behavior (subscription reconnect/transfer once Phase 2
  lands, in particular) is never exercised end-to-end by the automated suite.

## Code Quality Indicators
- **Linting**: `golangci-lint`/`staticcheck`/`gosec`/`govulncheck` are wired
  into the `Makefile` but not reliably available in every dev environment,
  and the Makefile's `lint`/`security-scan`/`vuln-check` targets silently
  no-op if the tool isn't installed rather than failing — fine for local dev,
  wrong for CI if this repo ever gets a CI pipeline (none currently exists in
  `.github/` beyond whatever's already there — not inventoried further here
  since it's out of this analysis's scope). `go vet`/`gofmt` are always run
  and are clean.
- **Code Style**: Consistent — table-driven tests, `fmt.Errorf("...: %w", err)`
  wrapping throughout, all logging through `internal/logger` (no stray
  `fmt.Print*`/`log.Print*`).
- **Documentation**: Good at the package/exported-symbol level; `README.md`
  now documents all 15 tools and every env var (verified via a mechanical
  diff against `SetupTools()`/`config.go` during the last hardening pass).

## Technical Debt

- **Tool-surface overlap**: `opcua_get_value` is a strict single-node subset
  of `opcua_read`; `opcua_browse` is a strict `max_depth=1` subset of
  `opcua_browse_nodes`. Both pairs work correctly, but the overlap means two
  ways to do the same thing are exposed to the LLM client, at the cost of
  the model's tool-context budget. Not yet resolved.
- **Diagnostic tools unguarded**: `opcua_debug_search`/`opcua_ensure_server_nodes`
  are internal diagnostic/repair tools, exposed as unguarded, always-on,
  model-facing MCP tools with no opt-in flag.
- **Dead config fields**: `SearchConfig.CacheTTL`/`MaxCacheSize`
  (`SEARCH_CACHE_TTL`/`SEARCH_MAX_CACHE_SIZE`) are parsed but never read
  anywhere else in the codebase (`internal/config/config.go:98-99`) — no
  TTL/eviction logic exists on `nodeCache`, which is an unbounded map.
  `SearchConfig.EnableCache` (`SEARCH_ENABLE_CACHE`) has exactly one usage,
  purely cosmetic (reported in `GetCacheStats()`), gating no actual behavior
  today. **Directly relevant to Phase 2**: the plan is to repurpose
  `EnableCache` as the real master toggle for the new read-through cache,
  and `CacheTTL` may become genuinely load-bearing then — worth resolving
  as part of that work, not before.
- **Documentation/implementation mismatch**: `OPCUAConfig.ServerCert`
  (`OPCUA_SERVER_CERT`, `internal/config/config.go:51`) is documented in
  `README.md` as if it enables server-certificate pinning/verification, but
  it's never read anywhere in `internal/opcua/client.go` — a latent
  security-documentation risk (an operator setting it believes they have
  server-cert verification and don't).
- **Docker hardening gaps**: no non-root `USER` directive, no `HEALTHCHECK`,
  unpinned builder base image tag (`cgr.dev/chainguard/go`, a rolling tag).
- **Known search-tier bug** (discovered during this hardening pass, not yet
  fixed): `GetNodeByBrowseName` and the Bleve-backed tiered search methods
  (`searchExact`/`searchWildcard`/`searchFuzzy`, `SearchNodes`,
  `SearchByDepth`, `SearchByNodeClass`) return zero hits for some queries
  that should match. Root-caused for `browse_name`: `NewDiscoveryService`
  maps that field with both a text and a keyword analyzer on the same path,
  and `bleve.NewMatchQuery` can't resolve a single analyzer for it. A couple
  of other query paths (numeric depth range, node_class exact term) also
  came back empty in testing without a confirmed root cause. The
  cache-fallback search paths (used whenever `SEARCH_ENABLE_SEARCH=false`)
  are unaffected and have real test coverage confirming they work correctly.

## Patterns and Anti-patterns

- **Good Patterns**: mutex+snapshot concurrency discipline
  (`internal/opcua/client.go`); interface-seam mocking instead of a live
  test server (`opcuaClient`); generation-tagged mark-and-sweep for a
  cache backed by a live external system (`internal/opcua/discovery.go`);
  consistent error wrapping; forced-stderr-in-stdio-mode logging to protect
  the MCP wire protocol.
- **Anti-patterns**: none rising to the severity of the items already listed
  as critical/high in prior audits (all of those have been fixed this
  session) — remaining issues are the technical-debt items above, all
  medium/low severity and already understood.
