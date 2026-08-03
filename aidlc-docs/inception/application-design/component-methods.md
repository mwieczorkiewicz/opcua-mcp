# Component Methods - Phase 2

Method signatures and high-level purpose only - business rules (e.g. exact
TTL-expiry comparison logic, batching algorithm details) are defined in
Functional Design, per-unit, during Construction.

## `store.Store` (`internal/store/store.go`)

```go
func Open(path string, timeout time.Duration) (*Store, error)
```
Opens (creating if absent) the bbolt file at `path`, with `timeout` passed
as `bbolt.Options.Timeout` (non-zero - see requirements.md FR-1.2). Creates
all 4 buckets via `CreateBucketIfNotExists`.

```go
func (s *Store) Close() error
```
Closes the underlying bbolt DB. Must be called exactly once, last in
shutdown order (requirements.md FR-1.5).

### `values` bucket
```go
func (s *Store) GetValue(nodeID string) (entry ValueEntry, ok bool, err error)
func (s *Store) PutValue(nodeID string, entry ValueEntry) error
func (s *Store) DeleteValue(nodeID string) error
```

### `typeinfo` bucket
```go
func (s *Store) GetTypeInfo(nodeID string) (entry TypeInfoEntry, ok bool, err error)
func (s *Store) PutTypeInfo(nodeID string, entry TypeInfoEntry) error
```

### `browse` bucket
```go
func (s *Store) GetBrowse(parentNodeID string) (entry BrowseEntry, ok bool, err error)
func (s *Store) PutBrowse(parentNodeID string, entry BrowseEntry) error
```

### `subscriptions` bucket
```go
func (s *Store) GetSubscription(id string) (intent SubscriptionIntent, ok bool, err error)
func (s *Store) PutSubscription(id string, intent SubscriptionIntent) error
func (s *Store) DeleteSubscription(id string) error
func (s *Store) ListSubscriptions() ([]SubscriptionIntent, error)
```

All `Get*` methods return `(zeroValue, false, nil)` for a missing key - not
an error, matching Go map-lookup idiom (`ok` pattern) rather than a typed
"not found" error, since a cache miss is an expected, common outcome here
(unlike e.g. `GetNodeInfo` elsewhere in this codebase, where "not found" is
closer to exceptional). `err` is reserved for real I/O/decode failures.

## `internal/store/types.go` (data model)

```go
type ValueEntry struct {
    Value           interface{}
    Status          string    // ua.StatusCode.String() - store doesn't depend on gopcua wire types directly
    SourceTimestamp time.Time
    ServerTimestamp time.Time
    ReceivedAt      time.Time // when this entry was written (for max_age_ms comparison)
}

type TypeInfoEntry struct {
    DataTypeID      uint32
    ValueRank       int32
    ArrayDimensions []uint32
    AccessLevel     byte
    UserAccessLevel byte
    CachedAt        time.Time
}

type BrowseEntry struct {
    References []BrowseReference
    CachedAt   time.Time
}

type BrowseReference struct {
    NodeID         string
    BrowseName     string
    DisplayName    string
    NodeClass      string
    TypeDefinition string
}

type SubscriptionIntent struct {
    ID         string
    NodeIDs    []string
    IntervalMs int
    CreatedAt  time.Time
}
```

## `opcua.CachingClient` (`internal/opcua/caching_client.go`)

```go
// valueTypeBrowseStore is the narrow slice of store.Store's surface
// CachingClient needs, mirroring the opcuaClient interface-seam pattern.
type valueTypeBrowseStore interface {
    GetValue(nodeID string) (store.ValueEntry, bool, error)
    PutValue(nodeID string, entry store.ValueEntry) error
    DeleteValue(nodeID string) error
    GetTypeInfo(nodeID string) (store.TypeInfoEntry, bool, error)
    PutTypeInfo(nodeID string, entry store.TypeInfoEntry) error
    GetBrowse(parentNodeID string) (store.BrowseEntry, bool, error)
    PutBrowse(parentNodeID string, entry store.BrowseEntry) error
}

func NewCachingClient(client *Client, cache valueTypeBrowseStore, cfg *config.SearchConfig) *CachingClient
```
`cfg.EnableCache` is the master toggle (requirements.md FR-3.7) - when
false, every method below delegates straight to `client` with no cache
interaction at all.

```go
func (c *CachingClient) Read(ctx context.Context, nodeIDs []string, maxAgeMs int) ([]ReadResult, error)
```
Per-node cache-or-live resolution (requirements.md FR-3.1/3.2). `ReadResult`
is a new type carrying value/status/timestamps plus `Source`
(`"live"|"cache"|"subscription"`) and `CachedAt`.

```go
func (c *CachingClient) Browse(ctx context.Context, nodeID string) ([]*ua.ReferenceDescription, error)
func (c *CachingClient) GetNodeTypeInfo(ctx context.Context, nodeID string) (*NodeTypeInfo, error)
func (c *CachingClient) Write(ctx context.Context, nodeID string, value interface{}) error
```
`Write` delegates to `client.Write`, then on success calls
`cache.DeleteValue(nodeID)` (requirements.md FR-3.6) unless the node is
currently subscribed (subscribed nodes self-correct from the next push, no
need to delete-then-immediately-repopulate).

## `opcua.SubscriptionManager` (`internal/opcua/subscription.go`)

```go
// subscriptionStore is the narrow slice of store.Store's surface
// SubscriptionManager needs.
type subscriptionStore interface {
    PutValue(nodeID string, entry store.ValueEntry) error // the pump goroutine writes here
    PutSubscription(id string, intent store.SubscriptionIntent) error
    DeleteSubscription(id string) error
    ListSubscriptions() ([]store.SubscriptionIntent, error)
}

func NewSubscriptionManager(client *Client, cache subscriptionStore, watcher *ReconnectWatcher, cfg *config.StoreConfig) *SubscriptionManager
func (m *SubscriptionManager) Start(ctx context.Context) error
```
`Start` performs the warm-start: loads persisted intent via
`ListSubscriptions`, re-issues `Subscribe`/`Monitor` for each
(requirements.md FR-2.5), then registers itself with `watcher` as the
rebuild callback for the permanent-death case (FR-2.6).

```go
func (m *SubscriptionManager) Stop() error
func (m *SubscriptionManager) Subscribe(ctx context.Context, nodeIDs []string, intervalMs int) (subscriptionID string, err error)
func (m *SubscriptionManager) Unsubscribe(ctx context.Context, subscriptionID string) error
func (m *SubscriptionManager) ListSubscriptions() ([]SubscriptionInfo, error)
```
`SubscriptionInfo` (exported, MCP-tool-facing) is distinct from
`store.SubscriptionIntent` (persistence-facing) - the tool-facing shape can
include live/derived fields (e.g. current connection status) the persisted
shape doesn't need.

## `opcua.ReconnectWatcher` (`internal/opcua/reconnect_watcher.go`)

```go
func NewReconnectWatcher(client *Client, onPermanentDeath func(ctx context.Context) error) *ReconnectWatcher
func (w *ReconnectWatcher) Start(ctx context.Context) error
func (w *ReconnectWatcher) Stop() error
```
`onPermanentDeath` is called exactly once per genuine `Closed`-after-having-
been-`Connected` transition; `SubscriptionManager.Start`'s rebuild logic is
registered here (component-dependency.md details the wiring).

## `mcp.Server` (extended, `internal/mcp/server.go`)

```go
func (s *Server) handleSubscribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
func (s *Server) handleUnsubscribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
func (s *Server) handleListSubscriptions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
```
Thin handlers delegating to `SubscriptionManager`, following the exact
pattern every existing handler already uses.

```go
func (s *Server) handleRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) // modified
```
Gains `max_age_ms` argument parsing; delegates to `CachingClient.Read`
instead of `Client.Read`; response formatting gains `source`/`cached_at`.
