# Code Generation Summary - Unit 2: Subscription Management

## Files created (application code)
- `internal/opcua/reconnect_watcher.go` - `ReconnectWatcher`: watches
  `opcua.ConnState` transitions via a `subscribingClient`'s
  `SetStateChangeChannel`, fires `onPermanentDeath` exactly once per
  `Connected → Closed` transition (BR-8), ignores transient states
  (`Connecting`/`Reconnecting`/`Disconnected`) and a `Closed` with no prior
  `Connected` (initial-connect failure).
- `internal/opcua/subscription.go` - the unit's core: `subscribingClient`/
  `subscriptionHandle`/`subscriptionStore` seam interfaces plus compile-time
  assertions; `NodeStatus`/`SubscriptionInfo`/`subscriptionRecord`/
  `intervalGroup` domain types; `SubscriptionManager` with `Start`/`Stop`,
  `warmStart` (rebuilds from persisted `store.SubscriptionIntent` before
  `Start` returns, BR-7), `rebuild` (invoked by the `ReconnectWatcher` on
  permanent death), `Subscribe`/`subscribeInternal` (BR-1 uuid IDs, BR-2
  interval-sharing/refcounting, BR-4 partial success with per-node
  rejection reasons), `Unsubscribe` (BR-3 whole-group removal, cancels the
  underlying gopcua subscription only when `RefCount` reaches 0),
  `ListSubscriptions`, and the `pump` goroutine (batches notifications by a
  ticker + size threshold, BR-6 log-and-continue on store write failure,
  routes incoming `*ua.DataChangeNotification`s by `ClientHandle`).
- `internal/opcua/mock_subscription_test.go` - `mockSubscribingClient`
  (implements `subscribingClient`) and `mockSubscriptionHandle` (implements
  `subscriptionHandle`, accepts every `Monitor` item by default with
  sequential `MonitoredItemID`s), following `mock_client_test.go`'s existing
  injectable-func-plus-call-counter style.
- `internal/opcua/reconnect_watcher_test.go` - 4 example-based tests:
  fires once on permanent death, ignores transient states, ignores `Closed`
  without a prior `Connected` (BR-8), resets and fires again after a second
  independent connected lifetime.
- `internal/opcua/subscription_test.go` - 9 example-based tests: subscribe
  success, partial failure (BR-4), all-rejected (tears down the empty
  group), interval sharing (asserts the underlying gopcua `Subscribe` is
  called once per distinct interval, not once per logical subscription),
  unsubscribe decrements refcount and cancels only at zero, unsubscribe of
  an unknown ID, warm-start restores persisted intent before `Start`
  returns, the pump batches notifications into the store, and a store write
  failure during the pump logs and continues rather than crashing (BR-6).
- `internal/opcua/subscription_pbt_test.go` - `TestSubscriptionManagerStatefulProperties`:
  a `pgregory.net/rapid` stateful test driving random Subscribe/Unsubscribe
  command sequences against a real `SubscriptionManager`, asserting after
  every command that `ListSubscriptions()` matches a simplified reference
  model and that each `intervalGroup.RefCount` equals the model's count of
  subscriptions at that interval (NFR Requirements Q2, PBT-06).

## Files modified (application code)
- `internal/opcua/client.go` - added `Subscribe` to the `opcuaClient`
  interface (exact signature match to `*opcua.Client`'s real method, so the
  existing `var _ opcuaClient = (*opcua.Client)(nil)` assertion needed no
  change); added a `stateCh chan<- opcua.ConnState` field guarded by the
  existing `mu`; added `SetStateChangeChannel`; `Connect()` now appends
  `opcua.StateChangedCh(stateCh)` to its options whenever a channel has been
  registered, read under the existing snapshot discipline; added
  `Client.Subscribe(ctx, params, notifyCh) (subscriptionHandle, error)`,
  a wrapper that snapshots connection state, delegates to the low-level
  `opcuaClient.Subscribe`, and upcasts the concrete `*opcua.Subscription`
  result to `subscriptionHandle`.
- `internal/opcua/mock_client_test.go` - added `subscribeFunc`/
  `subscribeCalls` and a `Subscribe` method to `mockOpcuaClient` so it keeps
  satisfying the extended `opcuaClient` interface.

## Test results
- `go build ./... && go vet ./... && gofmt -l .` - clean.
- `go test -race ./...` - all packages green; `internal/opcua` alone: 93
  tests passing (up from the pre-Unit-2 baseline).
- `go test -race ./...` total runtime: ~28-31s, dominated by
  `TestSubscriptionManagerStatefulProperties` (~20s for 100 rapid trials,
  each constructing a fresh bbolt-backed store and `SubscriptionManager`).
  Verified this matches `pgregory.net/rapid`'s own idiomatic single-level
  `rapid.Check` + `t.Repeat` structure (compared directly against the
  library's `example_statemachine_test.go`); the runtime is inherent
  per-trial I/O setup cost, not a structural misuse of the API, and stays
  well within normal `go test` timeouts.

## Deviations from the code generation plan
- **`NewSubscriptionManager` constructs its own `ReconnectWatcher`
  internally**, rather than taking one as a constructor parameter as
  `component-methods.md` originally specified. The watcher's callback must
  invoke the manager's own `rebuild` method, which is a chicken-and-egg
  problem for a parameter passed in before the manager exists; constructing
  it internally resolves this with no external API change.
- **`ListSubscriptions()` does not return an `error`**, unlike the signature
  implied by earlier design docs. In-memory map reads under `mu.RLock()`
  cannot fail, so the error return was dead weight.

Both deviations were called out at the time they were made and are
consistent with `component-methods.md`'s intent (BR-5/BR-7 behavior is
unchanged); no functional requirement is affected.
