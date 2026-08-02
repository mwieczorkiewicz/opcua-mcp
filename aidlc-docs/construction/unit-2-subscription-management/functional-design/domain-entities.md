# Domain Entities — Unit 2: Subscription Management

Answers referenced are from `unit-2-subscription-management-functional-design-plan.md`.

## `subscriptionRecord` (in-memory only — the persisted form is `store.SubscriptionIntent`)
```go
type subscriptionRecord struct {
    ID         string    // uuid.NewString() (Q1=A)
    NodeIDs    []string  // only the nodes the server actually accepted (Q4=A)
    IntervalMs int
    CreatedAt  time.Time
}
```

## `intervalGroup` (in-memory only, one per distinct `intervalMs` in active use — Q2=A)
```go
type intervalGroup struct {
    IntervalMs     int
    GopcuaSub      *opcua.Subscription // gopcua's own Subscription object, one per distinct interval
    RefCount       int                 // number of logical subscriptions currently using this group
    HandleToNodeID map[uint32]string   // gopcua client handle -> node ID, for routing incoming notifications
    NodeIDToHandle map[string]uint32   // reverse, for Unsubscribe's Unmonitor call
}
```
`RefCount` reaching zero (the last logical subscription referencing this
interval is removed) is what triggers tearing down the underlying gopcua
`Subscription` (`Cancel`) — see `business-rules.md` BR-3.

## `SubscriptionInfo` (exported, tool-facing — returned by `ListSubscriptions`)
```go
type SubscriptionInfo struct {
    ID         string
    NodeIDs    []string
    IntervalMs int
    CreatedAt  time.Time
}
```
Identical shape to `subscriptionRecord` today (no live/derived fields
beyond what's already tracked) — kept as a distinct exported type per
`application-design.md`'s original design (tool-facing vs. persistence/
internal-facing shapes are allowed to diverge later without changing the
persisted schema).

## `SubscriptionManager` (the component itself)
```go
type SubscriptionManager struct {
    client  *Client
    cache   subscriptionStore // narrow interface over store.Store, per application-design.md
    watcher *ReconnectWatcher
    cfg     *config.StoreConfig

    mu             sync.RWMutex // guards the two maps below, per this codebase's established convention
    subscriptions  map[string]*subscriptionRecord
    intervalGroups map[int]*intervalGroup
    nextHandle     uint32 // atomic counter; gopcua client handles must be unique across ALL monitored items, not just per-subscription

    notifyCh chan *ua.PublishNotificationData // one shared channel for every intervalGroup's GopcuaSub
    stopCh   chan struct{}
    wg       sync.WaitGroup // joined in Stop(), guarding the pump goroutine
}
```

## `ReconnectWatcher`
```go
type ReconnectWatcher struct {
    client           *Client
    onPermanentDeath func(ctx context.Context) error // SubscriptionManager's rebuild logic

    stateCh chan ua.ConnState // registered with Client via SetStateChangeChannel
    stopCh  chan struct{}
    wg      sync.WaitGroup
}
```

## `Client` extension (minor, per `application-design.md`'s "modified components" note)
```go
// SetStateChangeChannel registers ch to receive gopcua ConnState changes on
// every future (re)connect - Connect() includes opcua.StateChangedCh(ch) in
// its options whenever ch is non-nil, since Connect() constructs a brand-new
// underlying gopcua.Client each time it's called (including on rebuild after
// a permanent death) and gopcua's StateChangedCh option must be supplied at
// construction time.
func (c *Client) SetStateChangeChannel(ch chan<- ua.ConnState)
```
