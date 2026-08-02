# Code Generation Plan — Unit 3: Read-Through Caching & MCP Integration

**Workspace root**: `/Users/mikolajwieczorkiewicz/software-engineering/opcua-mcp`.
Brownfield — `internal/config/config.go`, `internal/mcp/server.go`,
`cmd/opcua-mcp.go` already exist (modify in-place);
`internal/opcua/caching_client.go` and its tests don't exist yet (create
new).

**Mode**: Autonomous (user stepped away, self-approving each step per the
operating rule recorded in `aidlc-docs/audit.md`). Plan approved by
self-review against `functional-design/`, `nfr-requirements/`,
`nfr-design/` before execution begins.

## Unit Context
- **Requirements covered**: FR-3, FR-4, FR-5.2, NFR-4, NFR-5.
- **Dependencies**: Unit 1 (`internal/store`), Unit 2
  (`SubscriptionManager`/`ReconnectWatcher`, already provide the
  `Source: "subscription"` provenance signal `CachingClient` reads).

## Steps

- [x] **Step 1 — Config cleanup (FR-5.2)**: remove `CacheTTL`/
      `MaxCacheSize` from `SearchConfig` in `internal/config/config.go`;
      update `internal/config/config_test.go` (remove their default-value
      assertions, `EnableCache`'s assertion stays).

- [x] **Step 2 — `CachingClient` core** (`internal/opcua/caching_client.go`,
      new): `valueTypeBrowseStore` interface + compile-time assertion,
      `ReadResult` type, `NewCachingClient`, `Read` (BR-1/BR-2), `Browse`
      (BR-1/BR-3), `GetNodeTypeInfo` (BR-1/BR-4), `Write` (BR-1/BR-5).

- [x] **Step 3 — `CachingClient` unit tests**
      (`internal/opcua/caching_client_test.go`, new): `EnableCache=false`
      bypass, subscribed-node unconditional serve, cache-hit-within-TTL,
      cache-miss/expired-falls-through-to-live, opportunistic cache
      population, browse cache hit/miss, typeinfo cache hit/miss, write
      invalidates non-subscribed cached entry, write leaves subscribed
      entry alone, store-error-falls-through-to-live for each method
      (NFR Design's error-handling pattern).

- [x] **Step 4 — `CachingClient` TTL-invariant property test**
      (`internal/opcua/caching_client_pbt_test.go`, new): NFR-3.2 —
      `cached ⟺ within TTL` across randomly generated `ReceivedAt`/
      `maxAgeMs` pairs.

- [x] **Step 5 — `mcp.Server` struct + constructor** (`internal/mcp/server.go`):
      `Server` gains `cachingClient *opcua.CachingClient` and
      `subscriptionManager *opcua.SubscriptionManager` fields;
      `NewServer` signature gains both as parameters (constructed by
      `cmd/opcua-mcp.go`, per `services.md`'s startup orchestration).

- [x] **Step 6 — 3 new MCP tools** (`internal/mcp/server.go`):
      `opcua_subscribe`/`opcua_unsubscribe`/`opcua_list_subscriptions` tool
      definitions in `SetupTools()` + `handleSubscribe`/`handleUnsubscribe`/
      `handleListSubscriptions` (BR-7/BR-8/BR-9).

- [x] **Step 7 — `opcua_read`/`opcua_write`/`opcua_browse_nodes` integration**:
      `handleRead` gains `max_age_ms` parsing, delegates to
      `cachingClient.Read`, response gains `source`/`cached_at` (FR-3.3);
      `handleWrite` delegates its type-info fetch and write call to
      `cachingClient` (BR-6); `browseNodesWithDepth` (used by
      `handleBrowseNodes`) delegates to `cachingClient.Browse` (BR-3).

- [x] **Step 8 — `mcp.Server` shutdown ordering** (`internal/mcp/server.go`):
      `setupGracefulShutdown` stops `SubscriptionManager` (joins pump) and
      `ReconnectWatcher` between `DiscoveryService.Stop()` and the HTTP
      server shutdown; `store.Close()` called strictly last, from
      `cmd/opcua-mcp.go` (BR-10) — `Server` needs a way to trigger this
      ordering (a `Store` reference or a shutdown-hook callback passed in
      at construction; exact mechanism decided during this step, following
      whatever keeps `store.Close()` demonstrably last without giving
      `Server` a store-CRUD surface it doesn't otherwise need).

- [x] **Step 9 — `cmd/opcua-mcp.go` wiring** (BR-10/BR-11): `store.Open`
      before `opcua.NewClient`; on `store.Open` failure, log and degrade
      (force `EnableCache=false`, construct `CachingClient`/skip
      `SubscriptionManager`/`ReconnectWatcher` construction entirely) per
      BR-11; on success, construct `CachingClient`, `ReconnectWatcher`,
      `SubscriptionManager`, start both after `Connect` succeeds (or
      immediately for stdio, matching existing lazy-connect semantics).

- [x] **Step 10 — `internal/mcp/server_test.go` updates**: adjust every
      `NewServer(...)` call site for the new parameters; add tests for the
      3 new tool handlers and the modified `handleRead`/`handleWrite`/
      `handleBrowseNodes` behavior, reusing the existing mock seams.

- [x] **Step 11 — Integration test suite** (NFR-4.1/4.2,
      `internal/opcua/subscription_integration_test.go`, new,
      `//go:build integration`): `testcontainers-go` starts the pinned
      `mcr.microsoft.com/iot/opc-ua-test-server:2.8` image; real
      `Client.Connect` against it; subscribe → observe a live value change
      → assert the `values` bucket updates with `Source: "subscription"`;
      read-through cache hit/miss/expiry for an unsubscribed node against
      the real server. Disconnect/reconnect scenario included only if it
      can be made deterministic (NFR Requirements' documented scope note)
      — outcome recorded in the code generation summary.

- [x] **Step 12 — `Makefile` `test-integration` target**: add
      `test-integration: go test -tags=integration ./...` (or equivalent),
      added to `.PHONY` and the `help` target's listing, alongside the
      existing `test`/`test-coverage` targets — additive only, no changes
      to any target the concurrent Docker Compose work already added.

- [x] **Step 13 — `README.md` minimal additive update** (NFR-5.1): a
      short note documenting `STORE_DB_PATH`'s volume-mount treatment
      (same pattern as the existing `search_index/` mount), added next to
      the existing `-v ./search_index:/search_index` examples — additive
      only, no rewrite of sections the concurrent custom-connector work
      already changed.

- [x] **Step 14 — Documentation summary**: create
      `aidlc-docs/construction/unit-3-read-through-caching-mcp-integration/code/summary.md`.

## Story/Requirement Traceability
Every step maps to `requirements.md` FR-3/FR-4/FR-5.2/NFR-4/NFR-5 — see
`unit-of-work-story-map.md`.

## Verification (deferred to Build and Test, noted here per this unit's scope)
`go build ./... && go vet ./... && gofmt -l . && go test -race ./...` must
stay green throughout (excluding the separately-gated `integration`-tagged
suite, which requires Docker and is run explicitly via
`make test-integration`).
