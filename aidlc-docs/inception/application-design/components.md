# Components - Phase 2

**Scope correction from `requirements.md`**: FR-1.3 listed a 5th bucket,
`nodes`, carried over from an earlier sketch about persisting
`DiscoveryService`'s node cache for startup warm-loading. No functional
requirement in this pass actually describes behavior using it - that's a
distinct feature (discovery-cache persistence) never asked for here. Store
scope in this design is **4 buckets**: `values`, `typeinfo`, `browse`,
`subscriptions`.

## `store.Store` (new package `internal/store`)
- **Purpose**: bbolt-backed persistence for cached values, node type info,
  browse results, and subscription intent.
- **Responsibilities**: Open/Close lifecycle (with the mandatory non-zero
  `Options.Timeout`), bucket creation on open, typed CRUD per bucket.
- **Interfaces**: Concrete type, no interface in `internal/store` itself
  (per Q4 - the interface(s) consumers need are defined next to the
  consumers, in `internal/opcua`, mirroring the `opcuaClient` pattern).
  `Store`'s own tests use a real bbolt instance against `t.TempDir()` -
  it's an embedded, in-process library, not a network dependency, so no
  mock is needed to keep its tests hermetic and fast.

## `opcua.CachingClient` (new file `internal/opcua/caching_client.go`)
- **Purpose**: read-through caching decorator wrapping `*Client` (per Q3) -
  `Client` itself stays a pure OPC-UA protocol wrapper; this component adds
  the cache-or-live decision.
- **Responsibilities**: serve `Read`/`Browse`/`GetNodeTypeInfo` from cache
  when fresh, otherwise delegate to `*Client` and opportunistically cache
  the result; invalidate the `values` entry for a node on a successful
  `Write` (per requirements.md FR-3.6).
- **Interfaces**: implements the same `Read`/`Write`/`Browse`/
  `GetNodeTypeInfo` surface `internal/mcp/server.go` already calls on
  `*Client` today, so `server.go`'s call sites change which concrete type
  they hold, not how they call it.

## `opcua.SubscriptionManager` (new file `internal/opcua/subscription.go`)
- **Purpose**: manage the lifecycle of OPC-UA subscriptions - create,
  cancel, list, persist intent, and batch-write incoming notifications to
  the cache.
- **Responsibilities**: `Client.Subscribe`/`Subscription.Monitor`
  orchestration; a pump goroutine batching `values`-bucket writes within
  `STORE_BATCH_WINDOW`/`STORE_BATCH_MAX_ITEMS`; persisting/loading intent
  from the `subscriptions` bucket; startup warm-start (re-subscribe from
  persisted intent, per requirements.md FR-2.5) and post-`ReconnectWatcher`
  rebuild (same code path, per FR-2.6).
- **Interfaces**: depends on `*Client` (for `Subscribe`/`Monitor`) and a
  `*ReconnectWatcher` (Q1=B - separate component).

## `opcua.ReconnectWatcher` (new file `internal/opcua/reconnect_watcher.go`)
- **Purpose**: isolate "detect the OPC-UA client has permanently died and
  needs rebuilding" from subscription management itself (Q1=B - testable in
  isolation, not entangled with subscription-specific logic).
- **Responsibilities**: watch `opcua.StateChangedCh` (or
  `StateChangedFunc`) for a transition into `Closed` after having been
  connected; on that transition, invoke a caller-supplied rebuild callback.
  Deliberately does **not** watch/react to `Connecting`/`Reconnecting`/
  transient `Disconnected` states - those are gopcua's own `AutoReconnect`
  machinery's job (verified in Phase 2 planning: existing subscriptions
  survive those transitions without intervention).

## `mcp.Server` (existing component, extended - not a new component)
- **New responsibilities**: 3 new tool handlers (`handleSubscribe`/
  `handleUnsubscribe`/`handleListSubscriptions`) delegating to
  `SubscriptionManager`; `handleRead`/`handleBrowseNodes` delegate to
  `CachingClient` instead of `*Client` directly, gaining `max_age_ms`
  handling.
- **Unchanged**: registration pattern (`SetupTools()`), transport/shutdown
  handling, all other existing tool handlers.
