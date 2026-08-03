# Code Generation Summary - Unit 3: Read-Through Caching & MCP Integration

Built in **autonomous mode** (user stepped away; self-decided every open
question rather than pausing, per the operating rule in `aidlc-docs/audit.md`,
"Unit 2 Approved - Autonomous Mode Enabled for Unit 3"). All 14 planned
steps completed; every self-decided point below was also flagged inline in
the corresponding functional/NFR design doc at the time it was made.

## Files created (application code)
- `internal/opcua/caching_client.go` - `CachingClient`: read-through
  caching decorator wrapping `*Client`. `Read` resolves each node
  independently (subscribed → always fresh from cache; unsubscribed →
  cache hit within `max_age_ms` or live), `Browse`/`GetNodeTypeInfo` check
  their respective buckets against `BrowseTTL`/`TypeInfoTTL`, `Write`
  invalidates a non-subscribed cached entry on success. Uses the existing
  `store.ValueEntry.Source` field (already written by Unit 2's pump) as
  the subscribed/cached signal - no dependency on `SubscriptionManager`.
- `internal/opcua/caching_client_test.go` - 12 example-based tests:
  `EnableCache=false` bypass, subscribed-unconditional-serve,
  cache-hit-within-TTL, cache-expired-falls-through-to-live (with
  re-caching verified), store-read-error-falls-through-to-live, browse
  cache hit/miss (with live-to-cache conversion verified), typeinfo cache
  hit/miss, write invalidates non-subscribed entry, write leaves
  subscribed entry alone, a failed write skips invalidation.
- `internal/opcua/caching_client_pbt_test.go` - `TestCachingClientReadTTLInvariant`
  (NFR-3.2, `pgregory.net/rapid`): the `cached ⟺ within TTL` invariant
  across randomly generated ages, drawn with a fixed safety margin around
  the TTL boundary to avoid the test's own wall-clock overhead producing a
  flaky boundary case (documented in the test's comment). Plus a dedicated
  example test for the `max_age_ms=0` edge case the property test
  deliberately excludes.
- `internal/opcua/subscription_integration_test.go` (`//go:build integration`)
  - `testcontainers-go`-driven suite against the real
  `mcr.microsoft.com/iot/opc-ua-test-server:2.8` image (reusing the pin
  already in `docker-compose.yml`): subscribing to the standard
  `Server_ServerStatus_CurrentTime` node (`i=2258`) pushes updates into
  the `values` bucket with `Source: "subscription"`; read-through caching
  hits/misses/expires correctly for the session-stable
  `Server_ServerStatus_StartTime` node (`i=2257`). Both tests run and pass
  against a real Docker daemon (not just this project's mocks) - verified
  during code generation, not merely compiled.

## Files modified (application code)
- `internal/config/config.go` - removed `SearchConfig.CacheTTL`/
  `MaxCacheSize` (dead fields per `code-quality-assessment.md`); no test
  changes needed since `config_test.go` never asserted their defaults.
- `internal/mcp/server.go` - `Server` gains `cachingClient`/
  `subscriptionManager`/`store` fields (the latter two nilable, per BR-11);
  `NewServer` gains 3 parameters; `opcua_read` gains `max_age_ms`;
  `handleRead`/`handleWrite`/`browseNodesWithDepth` delegate to
  `cachingClient` instead of `opcuaClient` directly; 3 new tool
  registrations + handlers (`handleSubscribe`/`handleUnsubscribe`/
  `handleListSubscriptions`); `setupGracefulShutdown` now stops
  `SubscriptionManager` (joining its pump) before the HTTP server shuts
  down, and closes the store strictly last.
- `internal/mcp/server_test.go` - updated both `NewServer(...)` call sites;
  added `newTestServerWithSubscriptions` helper (real unconnected
  `*opcua.Client` + real bbolt-backed store + started `SubscriptionManager`)
  and 6 new tests covering the 3 new handlers' argument validation,
  not-connected behavior, unavailable-without-store behavior, and
  `opcua_read`'s new `source`/`cached_at` fields on a cache hit.
- `cmd/opcua-mcp.go` - opens the store before constructing the OPC-UA
  client (BR-10); on `store.Open` failure, logs a warning and continues
  with `EnableCache` forced `false` and no `SubscriptionManager` (BR-11);
  constructs and warm-starts `SubscriptionManager` only when the store
  opened successfully, itself falling back to `nil` if `Start()` fails.
- `Makefile` - new `test-integration` target (`go test -tags=integration
  ./...`), added to `.PHONY` and `help`.
- `README.md` - documents the 3 new tools, `opcua_read`'s `max_age_ms`/
  `source`/`cached_at`, a new "Persistent Store Configuration" table
  (`STORE_*` variables, previously undocumented since Unit 1), the
  `STORE_DB_PATH` volume-mount example, `make test-integration`, and a new
  changelog entry - while fixing the now-stale `SEARCH_CACHE_TTL`/
  `SEARCH_MAX_CACHE_SIZE` rows left over from before this unit's config
  cleanup. Left the concurrently-updated Docker Compose/custom-connector
  sections untouched.

## Dependency changes
- `github.com/testcontainers/testcontainers-go` v0.43.0 (new, test-only,
  pre-approved in `requirements.md` Q7) - pulls in a large indirect
  dependency tree (Docker client, OpenTelemetry, etc.) as expected for this
  library; confined entirely to the `integration`-tagged test file, no
  production code imports it.

## Test results
- `go build ./... && go build -tags=integration ./... && go vet ./... &&
  go vet -tags=integration ./... && gofmt -l .` - clean.
- `go test -race ./...` (default, no Docker required) - all 6 packages
  green.
- `go test -tags=integration ./internal/opcua/... -run TestIntegration -v`
  - both integration tests pass against a real, locally-run Docker
  container (confirmed during this session, not just compiled).

## Deviations from the code generation plan / design docs
- **Step 8/9 mechanism** (left open in the plan): `Server` gained a
  `store *store.Store` field, used only for `Close()` during shutdown -
  not a CRUD surface, consistent with the plan's own stated constraint.
- **`NewServer`'s parameter list** deviates from `services.md`'s literal
  sketch (`cfg, cachingClient, subscriptionManager, discoveryService`):
  `DiscoveryService` continues to be constructed internally by `NewServer`
  exactly as before (no behavior change, no reason to churn it), and
  `opcuaClient`/`store` are both still passed in alongside
  `cachingClient`/`subscriptionManager`. `services.md` itself frames its
  startup orchestration as a sketch subject to refinement at construction
  time, not a locked contract.
- **`cmd/opcua-mcp.go`'s construction order** reorders `services.md`'s
  sketch slightly: `SubscriptionManager` is constructed and `Start()`-ed
  *before* `mcp.NewServer` is called (not after, as the sketch implied),
  so that a `Start()` failure can result in a genuinely `nil`
  `subscriptionManager` being passed into `NewServer` - passing a
  non-nil-but-failed manager in and trying to null it out afterward
  wouldn't un-wire the pointer `Server` had already stored.
- **Disconnect/reconnect integration scenario** (NFR Requirements' explicit
  conditional scope note): **descoped**, not implemented. Forcibly killing
  and restarting the test-server container mid-test and asserting
  `ReconnectWatcher`'s rebuild fires deterministically would need
  container-lifecycle timing this project has no existing pattern for and
  risks exactly the flakiness NFR-4.2 explicitly asks to avoid; the two
  implemented scenarios (subscription push, cache hit/miss/expiry) already
  exercise the real gopcua wire behavior end-to-end. Reconnect-specific
  logic itself (`ReconnectWatcher`'s state-transition handling) already has
  full example-based unit test coverage from Unit 2.
- **README `opcua_read` example node ID**: uses `ns=2;i=1,ns=2;i=2` per the
  existing pre-Unit-3 example rather than a real subscribable node, to
  avoid rewriting an example unrelated to this unit's actual changes.

No deviation affects any FR/BR's actual behavior - all are either
mechanism choices the design docs explicitly left open, or narrower
descoping of optional/conditional scope that was flagged as conditional
from the start.
