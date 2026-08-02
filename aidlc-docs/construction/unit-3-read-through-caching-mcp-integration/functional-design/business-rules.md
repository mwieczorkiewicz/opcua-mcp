# Business Rules — Unit 3: Read-Through Caching & MCP Integration

Built in autonomous mode — see `domain-entities.md`'s header note and
`aidlc-docs/audit.md`. Rules marked **(auto-decided)** replace what would
otherwise be a `[Answer]:` question.

## BR-1: `EnableCache` is a hard master switch (FR-3.7)
Every `CachingClient` method checks `cfg.EnableCache` first. When `false`,
the method delegates straight to the wrapped `*Client` with **zero** cache
reads or writes — not even an opportunistic write-through — so behavior is
byte-for-byte identical to pre-Unit-3 behavior. This is checked once per
call, not cached at construction time, so flipping the env var and
restarting takes effect immediately (no separate reload mechanism needed
since config is only loaded once at startup anyway).

## BR-2: `Read` per-node cache-or-live decision tree (FR-3.1/3.2/3.3)
For each requested node ID, independently:
1. `GetValue(nodeID)`. If `!ok` or `EnableCache == false`: go live (step 4).
2. If `ok` and `entry.Source == "subscription"`: return it unconditionally
   as `Source: "subscription"` — **never** consults `max_age_ms`, since
   Unit 2's pump keeps subscribed values continuously fresh (FR-3.1's
   literal wording: "unconditional on max_age_ms").
3. If `ok` and `entry.Source == "live"` (previously opportunistically
   cached): if `time.Since(entry.ReceivedAt) <= max_age_ms`, return it as
   `Source: "cache"`. Otherwise fall through to step 4 (expired).
4. Live: batch every node that reached this step into one
   `Client.Read` call (preserves the existing P1-1 partial-batch
   behavior — one bad node must not fail the others), then for each
   result `PutValue(nodeID, ValueEntry{..., Source: "live", ReceivedAt: now})`
   and return `Source: "live"`.

**(auto-decided)**: `max_age_ms == 0` (the default) means step 3 never
succeeds (`time.Since(...) <= 0` is only true for a same-instant write,
practically never) — matching FR-3.1/3.2's design intent that omitting
`max_age_ms` reproduces today's always-live behavior for unsubscribed
nodes, while subscribed nodes still short-circuit at step 2 regardless.

## BR-3: `Browse` checks the `browse` bucket first (FR-3.4)
`Browse(ctx, nodeID)`: if `EnableCache` and `GetBrowse(nodeID)` hits within
`BrowseTTL` (`time.Since(entry.CachedAt) <= browseTTL`), return
`entry.References` directly — zero live calls. Otherwise call
`Client.Browse`, convert each `*ua.ReferenceDescription` into a
`store.BrowseReference` (same five fields `handleBrowse` already extracts
today), `PutBrowse(nodeID, BrowseEntry{References: ..., CachedAt: now})`,
and return the converted slice.

## BR-4: `GetNodeTypeInfo` checks the `typeinfo` bucket first (FR-3.5)
`GetNodeTypeInfo(ctx, nodeID)`: if `EnableCache` and `GetTypeInfo(nodeID)`
hits within `TypeInfoTTL`, reconstruct and return a `*NodeTypeInfo` from
the cached `TypeInfoEntry` (see `domain-entities.md`'s namespace-0
reconstruction note) — zero live reads (this is the full fix for the H4
finding: P1-4 already removed the *in-request* duplicate fetch; this
removes the redundant work *across separate `Write()` calls to the same
node over time*). On miss/expiry, delegate to `Client.GetNodeTypeInfo`
(the existing 5-read sequence), cache the result, and return it.

## BR-5: Write invalidates the cache unless the node is subscribed (FR-3.6)
`Write(ctx, nodeID, value)`: delegate to `Client.Write` first (unchanged
validation/conversion behavior). On success only:
1. `GetValue(nodeID)`. If `ok` and `entry.Source != "subscription"`:
   `DeleteValue(nodeID)` — closes the write-then-read-back staleness
   window for a read-through-cached (not subscribed) node.
2. If `ok` and `entry.Source == "subscription"`: **skip the delete** — the
   next push from Unit 2's pump naturally corrects it, and deleting would
   just recreate a momentary gap a subscribed reader shouldn't see.
3. If `!ok`: nothing to invalidate.

**(auto-decided)**: this reads the existing entry rather than asking
`SubscriptionManager` directly whether the node is subscribed — reusing
the same provenance-in-`ValueEntry` signal `Read` already relies on (BR-2),
keeping `CachingClient` and `SubscriptionManager` mutually unaware of each
other, exactly as `services.md` recommended to avoid a circular-ish
dependency between the two new components.

## BR-6: `opcua_write`'s type-info fetch switches to the caching path (FR-3.5, incidental)
`handleWrite` currently calls `Client.GetNodeTypeInfo` directly (for its
detailed error/success messages) *and* `Client.Write` (which internally
calls `GetNodeTypeInfo` a second time for validation) — both switch to
`CachingClient.GetNodeTypeInfo`/`CachingClient.Write`. This means a node
written to repeatedly now benefits from BR-4's caching automatically, with
no new code path — `CachingClient.Write` still calls the real
`Client.Write` internally (unchanged validation semantics), it's only the
handler-level and internal type-info lookups that gain caching.

## BR-7: `opcua_subscribe`/`opcua_unsubscribe`/`opcua_list_subscriptions` are thin delegators (FR-4.1-4.3)
No new business logic beyond what `SubscriptionManager` (Unit 2) already
implements — `handleSubscribe`/`handleUnsubscribe`/`handleListSubscriptions`
parse/format only, following every existing tool handler's established
shape (`node_ids` parsing reuses `opcua.ParseNodeIDs`, same as
`opcua_read`/`opcua_write`).

## BR-8: `opcua_unsubscribe` by `node_ids` unsubscribes whole matching groups (FR-4.2, auto-decided — see domain-entities.md)
When called with `node_ids` instead of `subscription_id`: for every active
subscription (`ListSubscriptions()`) whose `NodeIDs` intersects the
requested set, unsubscribe that entire group — reaffirming Unit 2's BR-3
(whole-group-only), never a partial-node removal within a group. A caller
passing one node from a 5-node group removes all 5; the response's
`unsubscribed` list makes this explicit rather than silent.

## BR-9: Tool description distinctness (FR-4.4)
`opcua_subscribe`/`opcua_unsubscribe`/`opcua_list_subscriptions` get
descriptions naming the push-based/persisted-across-restart behavior
explicitly (e.g. "receives live push updates" vs. `opcua_read`'s
"reads a value now") so they're not confusable with each other or with
existing tools — not fixing the pre-existing `opcua_read`/`opcua_get_value`
overlap (out of scope, tracked as technical debt), just not adding a new
instance of it.

## BR-10: Startup/shutdown ordering (NFR-1.3, `services.md`)
`cmd/opcua-mcp.go`: `store.Open` before `opcua.NewClient`/`Connect`.
Shutdown (`mcp.Server.setupGracefulShutdown`): stop `DiscoveryService` →
stop `SubscriptionManager` (joins its pump goroutine) → stop
`ReconnectWatcher` → shut down the HTTP server if running → disconnect the
OPC-UA client → `store.Close()` strictly last. A write-after-close from an
in-flight batch must never happen — enforced purely by ordering (`Stop()`
join semantics already implemented in Unit 2), no new synchronization
needed in Unit 3 beyond calling things in the right order.

## BR-11: Store-open/write failures degrade gracefully, never block basic operation (NFR-1.2)
If `store.Open` fails at startup: **(auto-decided)** log the error via
`internal/logger` and continue startup with `CachingClient`/
`SubscriptionManager` constructed over a `nil`-safe degraded path —
concretely, `cmd/opcua-mcp.go` falls back to constructing `CachingClient`
with `cfg.Search.EnableCache` forced `false` for that process lifetime
(equivalent to BR-1's existing off-switch, reusing it rather than adding a
second code path) and skips `SubscriptionManager.Start`/`ReconnectWatcher.Start`
entirely (subscriptions require the store to persist intent — there's no
sensible in-memory-only fallback for FR-2.4/2.5). The server still serves
every non-cached, non-subscribed read/write/browse tool normally. This is
a genuine judgment call with no existing precedent to lean on, since Units
1/2 always assumed a working store — flagging here per the autonomous-mode
instruction rather than blocking on it.
