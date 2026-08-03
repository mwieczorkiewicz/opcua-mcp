# Application Design - Phase 2 (Consolidated)

Consolidates `components.md`, `component-methods.md`, `services.md`, and
`component-dependency.md` into a single reference. See those files for full
detail; this is the summary.

## Scope correction
`requirements.md` FR-1.3 listed a `nodes` bucket carried over from an
earlier sketch about discovery-cache warm-loading - never actually
requested in this pass's functional requirements. Dropped: store scope is
**4 buckets** (`values`, `typeinfo`, `browse`, `subscriptions`), not 5.

## Design decisions (from `application-design-plan.md`)
1. **`ReconnectWatcher` is a separate component**, not embedded in
   `SubscriptionManager` - testable in isolation.
2. **`store.Store` uses strongly-typed methods per bucket** (~11 methods
   total across 4 buckets), not a generic `Get[T]`/`Put[T]` API - consistent
   with this codebase's existing style (no generics used elsewhere;
   `Client`'s per-attribute methods are individually typed too).
3. **Read-through caching lives in a new `CachingClient` decorator**, not
   inside `Client` itself - `Client` stays a pure OPC-UA protocol wrapper;
   `mcp.Server` swaps which concrete type it calls for read/browse/typeinfo.
4. **Consumers depend on narrow, consumer-defined interfaces over
   `store.Store`** (`valueTypeBrowseStore`, `subscriptionStore`), mirroring
   the existing `opcuaClient` mock-seam pattern - `store.Store`'s own tests
   use a real bbolt instance in a temp dir (no mock needed there; bbolt is
   embedded/in-process, unlike the networked gopcua dependency).
5. **`SubscriptionManager` follows the established mutex+snapshot
   concurrency convention** (`Client.mu`/`snapshot()`,
   `DiscoveryService.cacheMutex`/`discoveryMu`) for its internal state.

## New components
| Component | File | Purpose |
|---|---|---|
| `store.Store` | `internal/store/store.go` (+ `types.go`) | bbolt persistence, 4 buckets |
| `opcua.CachingClient` | `internal/opcua/caching_client.go` | read-through caching decorator over `Client` |
| `opcua.SubscriptionManager` | `internal/opcua/subscription.go` | subscription lifecycle + persistence + notification pump |
| `opcua.ReconnectWatcher` | `internal/opcua/reconnect_watcher.go` | detect permanent client death, trigger rebuild |

## Modified components
| Component | Change |
|---|---|
| `mcp.Server` | 3 new tool handlers; `handleRead`/`handleBrowseNodes` call `CachingClient` instead of `Client` directly |
| `internal/config` | new `StoreConfig`; `SearchConfig.EnableCache` repurposed as real toggle, `CacheTTL`/`MaxCacheSize` removed |
| `cmd/opcua-mcp.go` | wires `store.Open`/`Close`, constructs the 4 new components, starts/stops them in the documented order |

## Key open item carried to Functional Design
How `CachingClient` learns "is this node currently subscribed" (for
`source="subscription"` responses) without a direct dependency on
`SubscriptionManager` - recommendation is to carry provenance in
`store.ValueEntry` itself (the pump writes `Source: "subscription"` when it
writes; `CachingClient` just reads whatever `Source` is already stored).
Confirm this when the relevant unit's Functional Design starts.

## Dependency/data-flow summary
`mcp.Server` → `CachingClient`/`SubscriptionManager`/`DiscoveryService` (all
compile-time dependencies, no message bus). `CachingClient` and
`SubscriptionManager` both depend on `Client` (OPC-UA) and `store.Store`
(via narrow interfaces). `SubscriptionManager` depends on
`ReconnectWatcher`, which depends on `Client`. No cyclic dependencies. See
`component-dependency.md` for the full diagram.
