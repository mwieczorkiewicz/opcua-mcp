# Code Generation Plan - Unit 2: Subscription Management

**Workspace root**: `/Users/mikolajwieczorkiewicz/software-engineering/opcua-mcp`.
Brownfield - `internal/opcua/client.go` and `internal/opcua/mock_client_test.go`
already exist (modify in-place); `internal/opcua/subscription.go`,
`reconnect_watcher.go`, and their test files don't exist yet (create new).

## Unit Context
- **Requirements covered**: FR-2 (`requirements.md`)
- **Dependencies**: Unit 1 (`internal/store`) for persistence
- **Key technical wrinkle resolved during planning**: gopcua's
  `Subscription.Monitor`/`Unmonitor` route by two *different* IDs -
  `ClientHandle` (we assign it, used to route incoming
  `*ua.DataChangeNotification.MonitoredItems[].ClientHandle` back to a node
  ID) vs. `MonitoredItemID` (server-assigned, returned in
  `MonitoredItemCreateResult`, required by `Unmonitor`). Both must be
  tracked per node. Also: gopcua's real `*opcua.Subscription` is a concrete
  struct with unexported fields - it can't be faked directly, so the
  mockable seam is a *new*, narrow `subscriptionHandle` interface
  (`Monitor`/`Unmonitor`/`Cancel`) that `*opcua.Subscription` satisfies
  implicitly (real signatures match exactly), separate from the existing
  low-level `opcuaClient` interface.

## Steps

- [x] **Step 1 - Extend `opcuaClient` interface**: add
      `Subscribe(ctx, *opcua.SubscriptionParameters, chan<- *ua.PublishNotificationData) (*opcua.Subscription, error)`
      to `internal/opcua/client.go`'s `opcuaClient` interface (matches
      `*opcua.Client`'s real signature exactly, so the existing
      `var _ opcuaClient = (*opcua.Client)(nil)` assertion continues to
      hold with no changes needed there).

- [x] **Step 2 - `Client` extensions**: in `client.go`, add:
      - `stateCh chan<- ua.ConnState` field (guarded by the existing `mu`).
      - `SetStateChangeChannel(ch chan<- ua.ConnState)` (sets `stateCh`
        under `mu.Lock()`).
      - `Connect()` includes `opcua.StateChangedCh(c.stateCh)` in its opts
        slice whenever `c.stateCh != nil` (read under `mu.RLock()` before
        building opts, matching the existing snapshot discipline).
      - `Subscribe(ctx, params, notifyCh) (subscriptionHandle, error)` -
        snapshots the connection, calls the underlying `opcuaClient.Subscribe`,
        returns the resulting `*opcua.Subscription` upcast to
        `subscriptionHandle` (defined in Step 3).

- [x] **Step 3 - Seam interfaces**: create `internal/opcua/subscription.go`
      (start of the file) with `subscribingClient` (Connect/
      SetStateChangeChannel/Subscribe - satisfied by `*Client`) and
      `subscriptionHandle` (Monitor/Unmonitor/Cancel - satisfied by
      `*opcua.Subscription`), plus compile-time assertions for both.

- [x] **Step 4 - `ReconnectWatcher`**: create
      `internal/opcua/reconnect_watcher.go` per
      `business-logic-model.md`'s watch-loop flowchart (BR-5 synchronous
      callback, BR-8 only-fire-on-first-Closed-after-Connected).

- [x] **Step 5 - `SubscriptionManager` core**: continue
      `subscription.go` - struct fields (`domain-entities.md`), `NewSubscriptionManager`,
      `Start`/`Stop` (BR-7 warm-start sequencing, joining the pump
      goroutine and registering with `ReconnectWatcher`).

- [x] **Step 6 - `Subscribe`/`Unsubscribe`/`ListSubscriptions`**: per
      `business-logic-model.md`'s flowcharts - BR-1 (uuid IDs), BR-2
      (interval sharing/refcounting), BR-3 (whole-group unsubscribe), BR-4
      (partial success).

- [x] **Step 7 - Notification pump**: the batching goroutine per
      `business-logic-model.md` - BR-6 (log-and-continue on store errors),
      routing incoming notifications via `ClientHandle`.

- [x] **Step 8 - Extend the mock seam**: create
      `internal/opcua/mock_subscription_test.go` - `mockSubscribingClient`
      (implements `subscribingClient`) and `mockSubscriptionHandle`
      (implements `subscriptionHandle`), with injectable funcs and call
      counters, following `mock_client_test.go`'s existing style.

- [x] **Step 9 - `ReconnectWatcher` unit tests**: create
      `internal/opcua/reconnect_watcher_test.go` - drives a fake
      `stateCh` through `Connected → Closed` (fires callback exactly once),
      `Connected → Reconnecting → Connected` (never fires), `Closed` without
      a prior `Connected` (never fires, BR-8).

- [x] **Step 10 - `SubscriptionManager` unit tests**: create
      `internal/opcua/subscription_test.go` - `Subscribe` success,
      `Subscribe` partial failure (BR-4), two `Subscribe` calls at the same
      interval share one gopcua `Subscription` (BR-2, assert call counts),
      `Unsubscribe` decrements refcount and only cancels at zero,
      `Start`'s warm-start rebuilds from persisted intent before returning
      (BR-7), the pump batches and writes to the store with
      `Source: "subscription"`, a store write failure during the pump
      logs and continues (BR-6) rather than crashing.

- [x] **Step 11 - Stateful property-based test**: create
      `internal/opcua/subscription_pbt_test.go` - `rapid`-driven command
      sequences (Subscribe/Unsubscribe) against a simplified reference
      model, asserting `ListSubscriptions()` matches the model and every
      `intervalGroup`'s `RefCount` invariant holds after each command
      (NFR Requirements Q2, PBT-06).

- [x] **Step 12 - Documentation summary**: create
      `aidlc-docs/construction/unit-2-subscription-management/code/summary.md`.

## Story/Requirement Traceability
Every step maps to `requirements.md` FR-2 - see `unit-of-work-story-map.md`.

## Verification (deferred to Build and Test, noted here per this unit's scope)
`go build ./... && go vet ./... && gofmt -l . && go test -race ./internal/opcua/...`
must be green before this unit is considered complete.
