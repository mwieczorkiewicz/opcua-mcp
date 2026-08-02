# Application Design Plan — Phase 2

## Context
`requirements.md` already names the two new components (`internal/store`,
`internal/opcua/subscription.go`'s `SubscriptionManager`) and their
high-level responsibilities. This plan works out their component
boundaries, method signatures, service-layer placement, and dependencies —
not detailed business logic (that's Functional Design, per-unit,
CONSTRUCTION phase).

## Plan

- [ ] Answer the design questions below
- [ ] Generate `components.md` — component definitions and responsibilities
- [ ] Generate `component-methods.md` — method signatures
- [ ] Generate `services.md` — service/orchestration definitions
- [ ] Generate `component-dependency.md` — dependency matrix + data flow
- [ ] Generate `application-design.md` — consolidated doc
- [ ] Validate design completeness and consistency

## Design Questions

### Question 1 — Reconnect-watcher component boundary
Should the "watch for the OPC-UA client going permanently `Closed` and
rebuild it" logic be its own component, or embedded inside
`SubscriptionManager`?

A) Embed it in `SubscriptionManager` (one component, simpler dependency graph — it's the only consumer of this behavior right now)

B) A separate `ReconnectWatcher` component that `SubscriptionManager` depends on (more testable in isolation, reusable if something else ever needs reconnect-awareness)

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 2 — Store API shape: generic or per-bucket typed methods
`internal/store` has 5 buckets (`values`, `typeinfo`, `browse`, `nodes`,
`subscriptions`), each storing a different Go type.

A) One generic `Store` with bucket-name + `interface{}`/generic-typed `Get`/`Put`/`Delete`/`List` methods (e.g. Go generics: `Get[T any](bucket, key string) (T, error)`) — less code, less type safety at the call site

B) A strongly-typed method set per bucket (e.g. `GetValue`/`PutValue`, `GetTypeInfo`/`PutTypeInfo`, ...) — more code (~20 methods), full type safety, self-documenting call sites

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 3 — Where does read-through caching logic live?
Caching could live inside `Client` itself (methods become cache-aware
internally) or as a separate layer wrapping `Client`.

A) Inside `Client` — `Read`/`Browse`/`GetNodeTypeInfo` become cache-aware directly, taking a `*store.Store` dependency. Simpler call sites (server.go doesn't change how it calls `Client`), but `Client` takes on a new responsibility beyond "OPC-UA protocol wrapper."

B) A separate decorator/wrapper (e.g. `CachingClient` or similar) that holds a `Client` and a `*store.Store`, implementing the same read/browse surface `internal/mcp/server.go` calls today — `Client` stays a pure protocol wrapper; `server.go` swaps which one it calls into.

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 4 — Store access: concrete type or interface (for the mock-seam pattern)
This project's established test pattern is mocking `Client`'s gopcua
dependency via a narrow interface (`opcuaClient`), not a live/simulated
server, for hermetic unit tests.

A) `SubscriptionManager`/the caching layer depend on `*store.Store` (concrete type) directly — `internal/store`'s own tests use a real bbolt instance against a temp dir (fast, in-process, no real mocking needed since bbolt itself is already an embedded/local library, not a network dependency)

B) Introduce a `store` interface (mirroring `opcuaClient`) so `SubscriptionManager`/caching-layer tests can inject a mock store instead of a real bbolt temp-dir instance

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 5 — SubscriptionManager's internal concurrency pattern
This project's established convention (`Client.mu`/`snapshot()`,
`DiscoveryService.cacheMutex`/`discoveryMu`) is: guard mutable shared state
with a mutex, read via a snapshot/copy rather than touching fields directly
under lock elsewhere.

A) Follow the same pattern for `SubscriptionManager`'s internal state (active subscriptions map, etc.) — no need to ask further, just confirming this is the expected convention to follow

B) I want a different concurrency approach for this component — I'll describe after [Answer]: tag below

[Answer]: A)
