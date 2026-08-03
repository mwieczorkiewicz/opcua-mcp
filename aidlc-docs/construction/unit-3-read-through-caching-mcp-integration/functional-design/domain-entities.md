# Domain Entities - Unit 3: Read-Through Caching & MCP Integration

Built in **autonomous mode** (user stepped away, explicitly authorized
self-resolving open questions using precedent from `requirements.md`/
`components.md`/`component-methods.md`/`services.md` and Unit 1/2
conventions - see `aidlc-docs/audit.md`, "Unit 2 Approved - Autonomous Mode
Enabled for Unit 3"). Design decisions that would normally be a
`[Answer]:` question are marked **(auto-decided)** below with their
rationale, in place of a separate Q&A file.

## `store.ValueEntry` (existing, Unit 1 - no change needed)
Already carries a `Source string` field (`"live"` or `"subscription"`),
written by Unit 2's pump as `"subscription"` for every push-based update.
**(auto-decided)**: this is exactly the provenance signal `services.md`
recommended ("store the provenance in `ValueEntry` itself") to let
`CachingClient` know whether a node is subscribed without holding a
reference to `SubscriptionManager` - already implemented, nothing to add.

## `ReadResult` (new, tool-facing - `internal/opcua/caching_client.go`)
```go
type ReadResult struct {
    NodeID          string
    Value           interface{}
    Status          string
    SourceTimestamp time.Time
    ServerTimestamp time.Time
    Source          string    // "live" | "cache" | "subscription" (FR-3.3)
    CachedAt        time.Time // zero if Source == "live"
}
```
Three-valued `Source` here is distinct from `store.ValueEntry.Source`
(two-valued: who *wrote* the cache entry). `ReadResult.Source` describes
how *this particular response* was produced:
- `"subscription"`: served from a `values`-bucket entry whose
  `ValueEntry.Source == "subscription"` - always used unconditionally,
  regardless of `max_age_ms` (FR-3.1).
- `"cache"`: served from a `values`-bucket entry whose
  `ValueEntry.Source == "live"` (i.e. previously opportunistically cached),
  within `max_age_ms` (FR-3.2).
- `"live"`: no usable cache entry; read directly from the OPC-UA server via
  `Client.Read`, then opportunistically cached with `Source: "live"`.

## `internal/store.BrowseReference` (existing, Unit 1 - reused directly)
```go
type BrowseReference struct {
    NodeID, BrowseName, DisplayName, NodeClass, TypeDefinition string
}
```
**(auto-decided)**: `component-methods.md` originally specified
`CachingClient.Browse` returning `[]*ua.ReferenceDescription` (matching
`Client.Browse`'s live signature exactly). On a cache hit there is no live
`*ua.ReferenceDescription` to return - reconstructing gopcua's real struct
(with its `*ua.ExpandedNodeID`/`*ua.QualifiedName` fields) from the flat
strings already stored in `BrowseEntry` would be lossy/fragile and gains
nothing, since every existing caller (`handleBrowse`/`handleBrowseNodes`)
immediately flattens the live result into the exact same five string
fields `BrowseReference` already has. `CachingClient.Browse` therefore
returns `([]store.BrowseReference, error)` directly - one shape, used
identically whether served from cache or converted from a live
`*ua.ReferenceDescription` slice.

## `NodeTypeInfo` (existing, `internal/opcua/client.go` - reused, reconstructed from `TypeInfoEntry` on a cache hit)
`store.TypeInfoEntry.DataTypeID` is a `uint32` (Unit 1, already committed).
**(auto-decided)**: reconstructing `NodeTypeInfo.DataType *ua.NodeID` from
this on a cache hit uses `ua.NewNumericNodeID(0, entry.DataTypeID)` -
namespace 0 is assumed, matching this codebase's own existing precedent:
`convertValueToOPCUAType`/`validateScalarValue`'s switches already operate
purely on `typeInfo.DataType.IntID()` everywhere else in `client.go`,
never checking namespace. This isn't a new gap introduced by caching; it's
the same simplification the write-validation path already lives with.

## `CachingClient` (new, `internal/opcua/caching_client.go`)
```go
// valueTypeBrowseStore is the narrow slice of store.Store's surface
// CachingClient needs, mirroring the opcuaClient/subscriptionStore
// interface-seam pattern already used in internal/opcua.
type valueTypeBrowseStore interface {
    GetValue(ctx context.Context, nodeID string) (store.ValueEntry, bool, error)
    PutValue(ctx context.Context, nodeID string, entry store.ValueEntry) error
    DeleteValue(ctx context.Context, nodeID string) error
    GetTypeInfo(ctx context.Context, nodeID string) (store.TypeInfoEntry, bool, error)
    PutTypeInfo(ctx context.Context, nodeID string, entry store.TypeInfoEntry) error
    GetBrowse(ctx context.Context, parentNodeID string) (store.BrowseEntry, bool, error)
    PutBrowse(ctx context.Context, parentNodeID string, entry store.BrowseEntry) error
}

type CachingClient struct {
    client *Client
    cache  valueTypeBrowseStore
    cfg    *config.SearchConfig // EnableCache (FR-3.7) + TTLs come from cfg.Store at construction
    typeInfoTTL time.Duration
    browseTTL   time.Duration
}

func NewCachingClient(client *Client, cache valueTypeBrowseStore, searchCfg *config.SearchConfig, typeInfoTTL, browseTTL time.Duration) *CachingClient
```
**(auto-decided)**: `component-methods.md`'s signature passed only
`*config.SearchConfig`, but the TTLs (`STORE_TYPEINFO_TTL`/`STORE_BROWSE_TTL`)
live on `config.StoreConfig`, not `SearchConfig` (per Unit 1's already-shipped
`config.go`). Passing the two TTL `time.Duration`s explicitly avoids giving
`CachingClient` an import-level dependency on the whole `StoreConfig`
struct just for two fields - `cmd/opcua-mcp.go`'s wiring passes
`cfg.Store.TypeInfoTTL`/`cfg.Store.BrowseTTL` at construction.

## Configuration cleanup (FR-3.7/FR-5.2, `internal/config/config.go`)
- `SearchConfig.EnableCache` (already exists) becomes the real master
  toggle read by `CachingClient` - no config shape change, just new
  behavior attached to an existing field.
- `SearchConfig.CacheTTL` and `SearchConfig.MaxCacheSize` (both currently
  unused dead fields per `code-quality-assessment.md`) are **removed**
  entirely, superseded by `StoreConfig.TypeInfoTTL`/`BrowseTTL`.

## New MCP tool shapes (`internal/mcp/server.go`)

### `opcua_subscribe`
- Request: `node_ids` (string, required - reuses `opcua.ParseNodeIDs`,
  same multi-format support as `opcua_read`), `interval_ms` (number,
  required).
- Response: `subscription_id`, `accepted` (node IDs), `rejected` (array of
  `{node_id, reason}`), `interval_ms`.

### `opcua_unsubscribe`
- Request: `subscription_id` (string) **or** `node_ids` (string) - exactly
  one must be provided. **(auto-decided, resolves FR-4.2's "or" ambiguity)**:
  `SubscriptionManager.Unsubscribe` only operates whole-group by ID (BR-3
  from Unit 2, unchanged). When `node_ids` is given instead, the handler
  calls `ListSubscriptions()`, finds every subscription whose `NodeIDs`
  intersects the given set, and unsubscribes each matched group *in whole*
  - never a partial-group removal. The response lists which subscription
  IDs were actually removed, so a caller passing `node_ids` can see the
  (possibly larger-than-expected) blast radius.
- Response: `unsubscribed` (array of subscription IDs actually removed).

### `opcua_list_subscriptions`
- Request: none.
- Response: array of `{subscription_id, node_ids, interval_ms, created_at}`
  - a direct format of `SubscriptionManager.ListSubscriptions()`'s
  `[]SubscriptionInfo` (Unit 2), already guaranteed consistent with
  persisted intent even immediately after a restart, per Unit 2's BR-7.

## `opcua_read` response shape change (FR-3.3, FR-3.9=Q9 no-compat-constraint)
Each element gains `source` (`"live"|"cache"|"subscription"`) and
`cached_at` (omitted/zero when `source == "live"`); existing fields
(`node_id`/`value`/`status`/`source_timestamp`/`server_timestamp`) are
unchanged. New optional request parameter `max_age_ms` (number, default
`0` - `0` means "no cache tolerance, must be subscribed or read live",
matching FR-3.1/3.2's literal wording).
