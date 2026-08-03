# NFR Design Patterns - Unit 2: Subscription Management

## Resilience: reconnect rebuild (already the pattern, restated here)
`ReconnectWatcher` + `SubscriptionManager.Start`'s warm-start logic
(Functional Design BR-5/BR-7/BR-8) together *are* this unit's resilience
pattern - a single-attempt rebuild on detected permanent client death,
relying on `Client`'s own retry/backoff rather than a second stacked policy
(NFR Requirements Q1).

## No client-side subscription/monitored-item cap (Q1=A)
Server-side limits (which vary per OPC-UA server implementation) are left
to the server to enforce; rejections surface through the same per-node
partial-failure path `Subscribe` already has (BR-4) rather than a second,
separate "limit exceeded" error path. One consistent way for a `Subscribe`
call to partially or fully fail, not two.

## Performance: batching (already the pattern, restated here)
`STORE_BATCH_WINDOW`/`STORE_BATCH_MAX_ITEMS` (Unit 1 config) bound the pump.
Interval-sharing (BR-2) already minimizes the number of live gopcua
`Subscription`s server-side.

## Concurrency pattern: mutex + snapshot (confirmed, no changes)
`SubscriptionManager.mu` guards `subscriptions`/`intervalGroups`/
`nextHandle`, following `Client.mu`/`DiscoveryService.cacheMutex`'s
established convention (Application Design Question 5). `ReconnectWatcher`
needs no mutex of its own - its only mutable state (`everConnected`) is
touched exclusively within its own single watch-loop goroutine.

## Testability pattern: extend the existing mock seam, don't add a new one
`opcuaClient` (from Phase 0/1) gains `Subscribe`/`Monitor`/`Unmonitor`/
`Cancel` methods for this unit's tests, rather than introducing a second,
parallel mock interface - consistent with how `internal/opcua/discovery_test.go`'s
`treeBrowser` already reused the same seam for `Browse`.
