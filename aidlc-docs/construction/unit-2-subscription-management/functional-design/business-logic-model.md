# Business Logic Model - Unit 2: Subscription Management

## Core workflow: `Subscribe`

```mermaid
flowchart TD
    Start(["Subscribe(ctx, nodeIDs, intervalMs)"])
    Lock["mu.Lock()"]
    FindGroup{"intervalGroups[intervalMs]<br/>exists?"}
    CreateGroup["client.Subscribe(ctx, &SubscriptionParameters{Interval: intervalMs}, notifyCh)<br/>-> new intervalGroup{RefCount: 0}"]
    BuildRequests["for each nodeID: allocate handle (nextHandle++),<br/>NewMonitoredItemCreateRequestWithDefaults"]
    Monitor["intervalGroup.GopcuaSub.Monitor(ctx, TimestampsToReturnBoth, requests...)"]
    ProcessResults["per BR-4: for each result -<br/>accepted -> HandleToNodeID/NodeIDToHandle;<br/>rejected -> collect for response"]
    GenID["id := uuid.NewString() (BR-1)"]
    Persist["store.PutSubscription(ctx, id, intent-with-accepted-nodes-only)"]
    IncRef["intervalGroup.RefCount++"]
    Unlock["mu.Unlock()"]
    Return(["return id, rejectedNodes, nil"])

    Start --> Lock --> FindGroup
    FindGroup -->|no| CreateGroup --> BuildRequests
    FindGroup -->|yes| BuildRequests
    BuildRequests --> Monitor --> ProcessResults --> GenID --> Persist --> IncRef --> Unlock --> Return
```

## Core workflow: `Unsubscribe`

```mermaid
flowchart TD
    Start(["Unsubscribe(ctx, subscriptionID)"])
    Lock["mu.Lock()"]
    Find{"subscriptions[id] exists?"}
    NotFound(["return error: not found"])
    Unmonitor["for each nodeID in record.NodeIDs:<br/>look up handle, GopcuaSub.Unmonitor(ctx, handle),<br/>remove from HandleToNodeID/NodeIDToHandle"]
    DecRef["intervalGroup.RefCount--"]
    RefZero{"RefCount == 0?"}
    Cancel["intervalGroup.GopcuaSub.Cancel(ctx)<br/>delete(intervalGroups, intervalMs)"]
    RemoveRecord["delete(subscriptions, id)"]
    DeletePersist["store.DeleteSubscription(ctx, id)"]
    Unlock["mu.Unlock()"]
    Return(["return nil"])

    Start --> Lock --> Find
    Find -->|no| NotFound
    Find -->|yes| Unmonitor --> DecRef --> RefZero
    RefZero -->|yes| Cancel --> RemoveRecord
    RefZero -->|no| RemoveRecord
    RemoveRecord --> DeletePersist --> Unlock --> Return
```

## Notification pump (one goroutine, started in `Start`, joined in `Stop`)

```mermaid
flowchart TD
    Loop["select on notifyCh / stopCh"]
    GotNotif["got *ua.PublishNotificationData"]
    Lookup["mu.RLock(): resolve which node(s) changed<br/>via the notification's monitored-item handles -><br/>HandleToNodeID (searched across all intervalGroups)"]
    Batch["accumulate into a batch, up to<br/>BatchMaxItems or until BatchWindow elapses"]
    Flush["for each batched (nodeID, value):<br/>store.PutValue(ctx, nodeID, ValueEntry{..., Source: \"subscription\"})"]
    OnErr["error? -> log via internal/logger, continue (BR-6)"]
    GotStop(["stopCh closed -> flush remaining batch, return"])

    Loop -->|notifyCh| GotNotif --> Lookup --> Batch
    Batch -->|window/size threshold reached| Flush --> OnErr --> Loop
    Batch -->|not yet| Loop
    Loop -->|stopCh| GotStop
```

### Text alternative (pump)
```
for {
  select {
  case notif := <-notifyCh:
    resolve notif's handles -> node IDs (mu.RLock during lookup)
    add (nodeID, value, timestamp) to pending batch
    if len(batch) >= BatchMaxItems: flush()
  case <-batchWindowTicker.C:
    if len(batch) > 0: flush()
  case <-stopCh:
    flush()  // drain whatever's pending before returning
    return
  }
}

flush():
  for each (nodeID, entry) in batch:
    if err := cache.PutValue(ctx, nodeID, entry); err != nil {
      logger.Warn("failed to cache subscription value", "node_id", nodeID, "error", err)  // BR-6: log and continue
    }
  clear batch
```

## `ReconnectWatcher`'s watch loop

```mermaid
flowchart TD
    Init["everConnected := false"]
    Loop["select on stateCh / stopCh"]
    GotState["got new ua.ConnState"]
    IsConnected{"state == Connected?"}
    MarkConnected["everConnected = true"]
    IsClosed{"state == Closed<br/>AND everConnected?"}
    Fire["onPermanentDeath(ctx) - synchronous (BR-5)<br/>everConnected = false (new lifetime starts on next Connected)"]
    GotStop(["stopCh closed -> return"])

    Init --> Loop
    Loop -->|stateCh| GotState --> IsConnected
    IsConnected -->|yes| MarkConnected --> Loop
    IsConnected -->|no| IsClosed
    IsClosed -->|yes| Fire --> Loop
    IsClosed -->|no| Loop
    Loop -->|stopCh| GotStop
```

### Text alternative
```
everConnected := false
for {
  select {
  case state := <-stateCh:
    if state == Connected: everConnected = true
    else if state == Closed && everConnected:
      onPermanentDeath(ctx)   // synchronous, BR-5
      everConnected = false   // reset for the new Client's lifetime after rebuild
  case <-stopCh:
    return
  }
}
```

## `SubscriptionManager.Start` (warm-start, BR-7)

```
Start(ctx):
  intents := store.ListSubscriptions(ctx)
  for each intent:
    # re-run the Subscribe logic against intent.NodeIDs/IntervalMs,
    # reusing intent.ID rather than generating a new uuid (this is a
    # restore, not a new logical subscription)
    rebuild intervalGroups/subscriptions in memory from intent
  watcher.Start(ctx)  # begin watching for future permanent death
  start pump goroutine
  return nil  # only after all of the above complete (BR-7)
```

`onPermanentDeath`'s rebuild callback (registered with `watcher` during
`Start`) performs the same "re-run Subscribe logic per persisted intent"
step against a freshly-`Connect`ed `Client` - it's the same warm-start
logic, invoked again after a permanent death rather than only at process
startup.
