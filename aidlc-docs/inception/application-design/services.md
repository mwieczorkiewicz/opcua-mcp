# Services - Phase 2

This project has no separate "service layer" package distinct from its
existing components (`internal/mcp.Server`, `internal/opcua.Client`,
`internal/opcua.DiscoveryService` already play this role) - Phase 2 follows
the same shape rather than introducing a new orchestration layer. The two
new components (`CachingClient`, `SubscriptionManager`) plus
`ReconnectWatcher` are each responsible for their own orchestration
internally; `cmd/opcua-mcp.go` and `mcp.Server` compose them, matching how
`Client`/`DiscoveryService` are composed today.

## Startup orchestration (`cmd/opcua-mcp.go`, extended)

```
1. config.Load() / cfg.Validate()          (existing)
2. logger.Init()                            (existing)
3. store.Open(cfg.Store.DBPath, cfg.Store.OpenTimeout)   (new)
4. opcua.NewClient(&cfg.OPCUA)                (existing)
5. opcua.NewCachingClient(client, store, &cfg.Search)     (new)
6. opcua.NewReconnectWatcher(client, ...)      (new - callback wired in step 8)
7. opcua.NewSubscriptionManager(client, store, watcher, &cfg.Store) (new)
8. mcp.NewServer(cfg, cachingClient, subscriptionManager, discoveryService) (modified)
9. Connect eagerly unless stdio                (existing, unchanged)
10. subscriptionManager.Start(ctx) / watcher.Start(ctx)   (new - after connect)
11. mcpServer.Start()                          (existing, unchanged)
```

Store opens **before** the OPC-UA client connects (step 3 before step 4),
since neither the caching layer nor the subscription manager can be
constructed without it, and store-open failures should surface before any
network I/O is attempted.

## Shutdown orchestration (`mcp.Server.setupGracefulShutdown`, extended)

Ordering is the load-bearing requirement here (requirements.md NFR-1.3):

```
1. Stop DiscoveryService                       (existing)
2. Stop SubscriptionManager (joins pump + warm-start goroutines)   (new)
3. Stop ReconnectWatcher                       (new)
4. Shutdown HTTP server if running              (existing)
5. Disconnect OPC-UA client                    (existing)
6. store.Close()                               (new - strictly last)
```

`store.Close()` must never race with an in-flight batch write from
`SubscriptionManager`'s pump goroutine - enforced by joining the pump
goroutine in step 2 before reaching step 6.

## Request-time orchestration: `opcua_read` with `max_age_ms`

```
1. mcp.Server.handleRead parses node_ids + max_age_ms
2. Delegates to CachingClient.Read(ctx, nodeIDs, maxAgeMs)
3. CachingClient, per node:
   a. If EnableCache is false: delegate straight to Client.Read, source="live"
   b. Else if node is subscribed (SubscriptionManager knows this): read from
      store's values bucket, source="subscription"
   c. Else if a values-bucket entry exists and is within maxAgeMs: source="cache"
   d. Else: live Client.Read, opportunistically PutValue, source="live"
4. mcp.Server formats the per-node results (value/status/timestamps/source/cached_at)
```

`CachingClient` needs a way to know "is this node currently subscribed" -
either a direct reference to `SubscriptionManager` (a query method,
e.g. `IsSubscribed(nodeID) bool`) or `SubscriptionManager` writing
subscribed-node values with a marker `CachingClient` can read back
(e.g. a `Source` field already stored in the `ValueEntry` itself, set to
"subscription" by the pump, avoiding a direct `CachingClient` →
`SubscriptionManager` dependency). **Recommend the latter** (store the
provenance in `ValueEntry` itself) to avoid a circular-ish dependency
between the two new components - flagging this as a Functional Design
detail to confirm when that unit starts, not deciding it as a hard
requirement here.

## Request-time orchestration: `opcua_subscribe`

```
1. mcp.Server.handleSubscribe parses node_ids + interval_ms
2. Delegates to SubscriptionManager.Subscribe(ctx, nodeIDs, intervalMs)
3. SubscriptionManager: Client.Subscribe (if no subscription exists yet) +
   Subscription.Monitor(nodeIDs) -> persists intent via store.PutSubscription
4. mcp.Server formats the subscription ID/confirmation
```
