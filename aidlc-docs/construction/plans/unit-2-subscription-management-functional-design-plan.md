# Functional Design Plan — Unit 2: Subscription Management

## Plan
- [ ] Answer the questions below
- [ ] Generate `business-logic-model.md`
- [ ] Generate `business-rules.md`
- [ ] Generate `domain-entities.md`

## Context
This is the most architecturally complex unit (per `execution-plan.md`'s
risk assessment) — it's where the verified gopcua v0.9.0 reconnect model
(Phase 2 planning) actually gets used. Six genuine open questions, all
consequential to the design.

## Questions

### Question 1 — Subscription ID generation
`Subscribe(ctx, nodeIDs, intervalMs)` returns a `subscriptionID` — this
project's own ID, not gopcua's server-assigned (and reconnect-unstable)
`SubscriptionID`.

A) `github.com/google/uuid` (already an indirect dependency via `mcp-go`/`gopcua` - promoting it to direct *use*, not a new dependency, same precedent as bbolt) — `uuid.NewString()` per logical subscription

B) A simple incrementing counter persisted alongside intent (e.g. `sub-1`, `sub-2`, ...) — no new dependency at all, but needs its own persisted "next ID" bookkeeping

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 2 — Mapping logical subscriptions to gopcua `*opcua.Client.Subscribe()` calls
gopcua's publishing interval (`SubscriptionParameters.Interval`) is set
**per gopcua `Subscription`**, not per monitored item. If two logical
subscriptions (two separate `Subscribe()` tool calls) request the *same*
`intervalMs`, should they share one underlying gopcua `Subscription`
(with `Monitor` adding items to it), or does every logical `Subscribe()`
call always get its own gopcua `Subscription`?

A) One gopcua `Subscription` per distinct `intervalMs` value, shared across logical subscriptions requesting that interval — fewer gopcua subscriptions/server-side resources, more bookkeeping (need an interval→gopcua-Subscription map, and reference-counting to know when a gopcua Subscription can be torn down)

B) One gopcua `Subscription` per logical `Subscribe()` call, always — simpler bookkeeping (1:1 mapping, no reference counting), more gopcua-level subscriptions if many logical subscriptions happen to share an interval (rarely a real problem at this project's confirmed scale of thousands of nodes/subscriptions, not thousands of *distinct intervals*)

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 3 — `Unsubscribe` semantics
`Unsubscribe(ctx, subscriptionID)` takes a single ID (per
`component-methods.md`).

A) Whole-group only — removes every node in that logical subscription at once; there's no partial/per-node unsubscribe API. If a caller wants to drop one node from a 5-node subscription, they `Unsubscribe` the whole group and `Subscribe` again with the remaining 4.

B) Add a variant that also accepts specific node IDs to partially unsubscribe from a group.

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 4 — Partial failure within one `Subscribe()` call
Per the Phase 2 gopcua research, `Subscription.Monitor`'s response
(`resp.Results[]`) already gives per-item status — some requested node IDs
in one `Subscribe(nodeIDs, intervalMs)` call could be accepted while others
are rejected by the server (bad node ID, access denied, etc.).

A) Partial success, mirroring P1-1's `Client.Read` fix — `Subscribe` succeeds for whatever nodes the server accepted, persists intent for only those, and returns per-node status alongside the subscription ID (not a hard all-or-nothing failure)

B) All-or-nothing — if any requested node is rejected, the whole `Subscribe()` call fails and nothing is persisted/monitored

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 5 — `ReconnectWatcher`'s callback execution model
`component-dependency.md` flagged this as open: when `onPermanentDeath` fires, does it run synchronously in the watcher's own goroutine, or does the watcher spawn a new goroutine for it?

A) Synchronous in the watcher's goroutine — the watcher's `Start` loop blocks for the duration of the rebuild; simpler (no extra goroutine lifecycle to manage), acceptable since a permanent-death rebuild is already a rare, slow-path event where blocking briefly longer doesn't matter (there's nothing else for the watcher to watch while its own client is dead anyway)

B) The watcher spawns a new goroutine for each `onPermanentDeath` invocation, returning immediately — more complex (need to guard against overlapping rebuilds if death is somehow detected twice), no clear benefit given A's reasoning

[Answer]: A)

### Question 6 — Pump goroutine failure handling
If a `store.PutValue` call fails inside the notification-batching pump
(e.g., disk full):

A) Log the error (via `internal/logger`) and continue processing subsequent notifications — a store write failure must not crash subscription delivery or the pump goroutine itself (matches requirements.md NFR-1.2: caching/subscriptions are an enhancement, not a hard dependency for basic operation)

B) Stop the pump and surface the error somewhere more prominent — describe after [Answer]: tag below

[Answer]: A)
