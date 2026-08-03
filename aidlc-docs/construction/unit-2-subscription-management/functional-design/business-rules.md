# Business Rules - Unit 2: Subscription Management

## BR-1: Subscription IDs are UUIDs (Q1=A)
Every logical subscription gets `uuid.NewString()` as its ID
(`github.com/google/uuid`, promoting an existing indirect dependency to
direct use - not a new dependency, same precedent as `bbolt`). Never a
gopcua server-assigned `SubscriptionID` - those are meaningless across a
reconnect/restart (verified during Phase 2 planning).

## BR-2: One gopcua `Subscription` per distinct interval, reference-counted (Q2=A)
- `Subscribe(nodeIDs, intervalMs)` looks up `intervalGroups[intervalMs]`.
  If absent, it creates a new gopcua `Subscription` via `client.Subscribe`
  with that interval, registered under the shared `notifyCh`.
- Every node in the call gets a fresh, globally-unique client handle
  (`nextHandle`, atomically incremented) and a `MonitoredItemCreateRequest`
  via `ua.NewMonitoredItemCreateRequestWithDefaults`, batched into one
  `intervalGroup.GopcuaSub.Monitor(...)` call.
- `intervalGroup.RefCount` increments by 1 per logical subscription that
  references this interval (not per node) - two logical subscriptions at
  the same interval share one `RefCount` of 2, not one each per node.
- Tearing down: when `Unsubscribe` drops the last logical subscription
  referencing an interval (`RefCount` reaches 0), the `intervalGroup`'s
  gopcua `Subscription.Cancel(ctx)` is called and the group is removed from
  `intervalGroups`. A shared interval with `RefCount > 0` is never
  cancelled just because one of its logical subscriptions is removed.

## BR-3: `Unsubscribe` is whole-group only (Q3=A)
`Unsubscribe(ctx, subscriptionID)` removes every node in that logical
subscription's `NodeIDs` at once - no partial/per-node removal API. A
caller wanting to drop one node from a group unsubscribes the whole group
and re-subscribes with the remaining nodes.

## BR-4: `Subscribe` succeeds partially (Q4=A)
Mirrors the P1-1 `Client.Read` fix. After `Monitor`'s response, each
requested node ID gets its own outcome from `resp.Results[]`:
- **Accepted**: added to `intervalGroup.HandleToNodeID`/`NodeIDToHandle`,
  included in the returned `subscriptionRecord.NodeIDs` and the persisted
  `store.SubscriptionIntent`.
- **Rejected**: excluded from both; the rejection (node ID + status) is
  returned to the caller alongside the subscription ID, never silently
  dropped.
- **All rejected**: still not a hard failure at the `Subscribe` level - an
  empty-`NodeIDs` `subscriptionRecord`/intent could result, though the
  caller-facing response should make an all-rejected outcome obvious
  (Functional Design flags this for Code Generation to handle clearly, not
  as a silent empty success).

## BR-5: `ReconnectWatcher`'s callback runs synchronously (Q5=A)
`onPermanentDeath` executes directly in the watcher's own state-watching
goroutine - no separate goroutine spawned per invocation. The watcher's
`Start` loop simply blocks for the rebuild's duration. Acceptable because:
permanent death is rare, there's nothing else useful for the watcher to
observe while its own `Client` is dead, and this avoids needing to guard
against overlapping/concurrent rebuild attempts.

## BR-6: Pump goroutine never crashes on a store write failure (Q6=A)
Every `store.PutValue` call inside the batching pump is wrapped: on error,
log via `internal/logger` (never `fmt.Print*`) and continue to the next
notification. A disk-full or similar store failure degrades to "no caching
of new values" - it never stops delivery processing or panics the pump
goroutine (requirements.md NFR-1.2).

## BR-7: Startup warm-start populates in-memory state synchronously
`SubscriptionManager.Start(ctx)` loads all persisted intent
(`store.ListSubscriptions`) and re-issues `Subscribe`/`Monitor` for each,
rebuilding `subscriptions`/`intervalGroups` **before `Start` returns**.
Since `cmd/opcua-mcp.go`'s startup sequence (per `services.md`) doesn't
begin serving MCP requests until after `Start` returns, `ListSubscriptions()`
reading the in-memory `subscriptions` map (not the store directly) is
always consistent with persisted intent by the time any tool call can
reach it - no separate "read-through the store for freshness" logic is
needed for `ListSubscriptions` itself.

## BR-8: `ReconnectWatcher` only acts on `Connected → Closed`, never other transitions
Reaffirming the Application Design decision: `Connecting`/`Reconnecting`/
transient `Disconnected` states are gopcua's own `AutoReconnect` machinery's
responsibility (verified in Phase 2 planning to already keep existing
`*Subscription` objects working). The watcher tracks only whether it has
ever observed `Connected`, and fires `onPermanentDeath` on the *first*
`Closed` observed after that - not on every `Closed` (there's only one
meaningful "the client is dead" transition per client lifetime; once dead,
a rebuild produces an entirely new `Client`/`Subscription` set, at which
point the watcher's "have we seen Connected" state resets for the new
lifetime).
