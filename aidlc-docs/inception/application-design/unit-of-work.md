# Units of Work - Phase 2

Approved decomposition (`unit-of-work-plan.md`, all 5 questions confirmed
the proposal as-is): 3 units, built strictly sequentially, solo-contributor
work, same deployment model across all units, domain boundaries mirror this
project's existing `internal/{store,opcua,mcp}` package structure.

## Unit 1 - Persistent Store
- **Responsibility**: bbolt-backed persistence layer - the foundational
  dependency every other unit needs.
- **Components** (from `application-design.md`): `store.Store`
  (`internal/store/store.go`, `internal/store/types.go`).
- **Maps to**: requirements.md FR-1 (all sub-items), FR-5.1 (`StoreConfig`).
- **Depends on**: nothing new (only the existing `internal/config` pattern
  to follow, and the newly-promoted-to-direct `go.etcd.io/bbolt`).
- **Deliverables**: `store.Open`/`Close`, 4 buckets, typed CRUD methods,
  `StoreConfig` added to `internal/config`, unit tests (real bbolt in
  `t.TempDir()`) + PBT round-trip tests for JSON encode/decode (NFR-3.1).

## Unit 2 - Subscription Management
- **Responsibility**: push-based subscriptions, persisted intent, reconnect
  handling.
- **Components**: `opcua.SubscriptionManager`
  (`internal/opcua/subscription.go`), `opcua.ReconnectWatcher`
  (`internal/opcua/reconnect_watcher.go`).
- **Maps to**: requirements.md FR-2 (all sub-items).
- **Depends on**: Unit 1 (`store.Store` via the `subscriptionStore`
  interface).
- **Deliverables**: `Subscribe`/`Unsubscribe`/`ListSubscriptions`, the
  notification-batching pump, startup warm-start + permanent-death rebuild
  (same code path), unit tests (mocked via the existing `opcuaClient` seam
  extended with `Subscribe`/`Monitor`/state-channel support) + stateful PBT
  (NFR-3.3).

## Unit 3 - Read-Through Caching & MCP Integration
- **Responsibility**: expose everything above through MCP - the
  user-visible surface of Phase 2.
- **Components**: `opcua.CachingClient`
  (`internal/opcua/caching_client.go`), `mcp.Server` changes (3 new tool
  handlers + `handleRead`/`handleBrowseNodes` modifications), `cmd/opcua-mcp.go`
  wiring, `Dockerfile`/`README.md`/`Makefile` updates, the integration-test
  suite (testcontainers-go + Microsoft OPC-UA test server).
- **Maps to**: requirements.md FR-3, FR-4, FR-5.2, NFR-4 (integration
  testing), NFR-5 (deployment).
- **Depends on**: Unit 1 (`store.Store`) and Unit 2 (new tools delegate to
  `SubscriptionManager`; `CachingClient` needs the "is subscribed" signal
  per `services.md`'s provenance-in-`ValueEntry` recommendation).
- **Deliverables**: `CachingClient`, 3 new MCP tools, `opcua_read`/
  `opcua_browse_nodes` integration, config cleanup (`EnableCache`
  repurposed, dead fields removed), Docker/README updates, unit tests +
  the full integration-test suite (this is where end-to-end
  subscribe/reconnect/cache behavior actually gets verified against a real
  server, per requirements.md Q7).

## Code Organization
No new top-level directory structure - units map directly onto this
project's existing `internal/{store,opcua,mcp}` package layout (brownfield,
following `code-generation.md`'s existing-structure-pattern guidance rather
than inventing a new one).
