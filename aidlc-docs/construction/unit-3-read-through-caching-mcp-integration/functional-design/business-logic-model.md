# Business Logic Model - Unit 3: Read-Through Caching & MCP Integration

## `CachingClient.Read` (BR-1, BR-2)

```mermaid
flowchart TD
    A[Read ctx, nodeIDs, maxAgeMs] --> B{EnableCache?}
    B -- no --> L[Client.Read all nodeIDs, Source=live]
    B -- yes --> C[Per node: GetValue nodeID]
    C --> D{Found?}
    D -- no --> M[mark node for live batch]
    D -- yes --> E{entry.Source == subscription?}
    E -- yes --> F[return entry, response Source=subscription]
    E -- no --> G{time.Since ReceivedAt <= maxAgeMs?}
    G -- yes --> H[return entry, response Source=cache]
    G -- no --> M
    M --> L
    L --> N[PutValue Source=live for each live result]
    N --> O[return response Source=live]
```

## `CachingClient.Browse` (BR-3)

```mermaid
flowchart TD
    A[Browse ctx, nodeID] --> B{EnableCache and GetBrowse hit within BrowseTTL?}
    B -- yes --> C[return cached References]
    B -- no --> D[Client.Browse nodeID]
    D --> E[convert to BrowseReference slice]
    E --> F[PutBrowse nodeID, CachedAt=now]
    F --> G[return converted References]
```

## `CachingClient.GetNodeTypeInfo` (BR-4)

```mermaid
flowchart TD
    A[GetNodeTypeInfo ctx, nodeID] --> B{EnableCache and GetTypeInfo hit within TypeInfoTTL?}
    B -- yes --> C[reconstruct NodeTypeInfo from TypeInfoEntry]
    B -- no --> D[Client.GetNodeTypeInfo - existing 5-read sequence]
    D --> E[PutTypeInfo nodeID, CachedAt=now]
    E --> F[return NodeTypeInfo]
    C --> F
```

## `CachingClient.Write` (BR-5)

```mermaid
flowchart TD
    A[Write ctx, nodeID, value] --> B[Client.Write - unchanged validation]
    B --> C{error?}
    C -- yes --> Z[return error, no cache interaction]
    C -- no --> D{EnableCache?}
    D -- no --> Y[return nil]
    D -- yes --> E[GetValue nodeID]
    E --> F{found and Source != subscription?}
    F -- yes --> G[DeleteValue nodeID]
    F -- no --> Y
    G --> Y
```

## `opcua_subscribe` tool handler (BR-7)

```mermaid
flowchart TD
    A[handleSubscribe] --> B[ParseNodeIDs node_ids]
    B --> C[SubscriptionManager.Subscribe ctx, nodeIDs, intervalMs]
    C --> D[format subscription_id, accepted, rejected]
```

## `opcua_unsubscribe` tool handler (BR-7, BR-8)

```mermaid
flowchart TD
    A[handleUnsubscribe] --> B{subscription_id given?}
    B -- yes --> C[SubscriptionManager.Unsubscribe ctx, subscription_id]
    C --> D[return unsubscribed = subscription_id]
    B -- no --> E[ParseNodeIDs node_ids]
    E --> F[ListSubscriptions - find groups intersecting node_ids]
    F --> G[Unsubscribe each matched group]
    G --> H[return unsubscribed = matched IDs]
```

## `opcua_list_subscriptions` tool handler (BR-7)

```mermaid
flowchart TD
    A[handleListSubscriptions] --> B[SubscriptionManager.ListSubscriptions]
    B --> C[format array of subscription_id, node_ids, interval_ms, created_at]
```

## Startup/shutdown ordering (BR-10, BR-11)

```mermaid
flowchart TD
    S1[config.Load / Validate] --> S2[logger.Init]
    S2 --> S3[store.Open DBPath, OpenTimeout]
    S3 --> S4{store.Open error?}
    S4 -- yes --> S5[log error, degrade: EnableCache=false, skip SubscriptionManager/ReconnectWatcher Start]
    S4 -- no --> S6[opcua.NewClient]
    S5 --> S6
    S6 --> S7[NewCachingClient client, store, cfg]
    S7 --> S8[NewReconnectWatcher + NewSubscriptionManager if store healthy]
    S8 --> S9[mcp.NewServer cachingClient, subscriptionManager, discovery]
    S9 --> S10[Connect eagerly unless stdio]
    S10 --> S11[subscriptionManager.Start / watcher.Start if store healthy]
    S11 --> S12[mcpServer.Start]
```

```mermaid
flowchart TD
    T1[shutdown signal] --> T2[discovery.Stop]
    T2 --> T3[subscriptionManager.Stop - joins pump goroutine]
    T3 --> T4[reconnectWatcher.Stop]
    T4 --> T5[httpServer.Shutdown if running]
    T5 --> T6[opcuaClient.Disconnect]
    T6 --> T7[store.Close - strictly last]
```
