# NFR Design Patterns — Unit 3: Read-Through Caching & MCP Integration

Built in autonomous mode. Items marked **(auto-decided)** resolve the two
security questions `requirements.md` explicitly deferred to this stage.

## SECURITY-01: no at-rest encryption for the bbolt file (auto-decided)
**Decision**: not required. **Rationale**: (1) this is an experimental/
lab-deployed tool, not a managed production service (per the
resiliency-extension answers already on file); (2) the cached data
(process values, node type metadata, browse results) is exactly what the
OPC-UA connection itself already transmits, and that connection defaults
to `OPCUA_SECURITY_POLICY=None` in this project — encrypting the *cache*
of data that arrived over an *unencrypted* wire by default would be
inconsistent, protecting data at rest more strongly than it's protected
in transit; (3) the bbolt file already gets `0600` permissions
(`store.Open`, Unit 1) — OS-level file permissions are the same tier of
protection this project's other local state (e.g. `search_index/`) gets
today. If a deployment needs stronger guarantees, the standard answer is
disk-level encryption (LUKS/FileVault/cloud volume encryption) outside
the application, not a bbolt-specific feature.

## SECURITY-13: no additional integrity/audit layer (auto-decided)
**Decision**: not required beyond what bbolt already provides. bbolt is a
single-writer, ACID, copy-on-write B+tree — every `Update` is atomic and
durable (fsync'd) by default, which is already stronger integrity than
this project has for any other local state. Adding a checksum/audit-log
layer on top would protect against threats (disk corruption, tampering)
that aren't in this project's threat model (single-process, single-user,
lab-deployed, per the resiliency-extension answers).

## Resilience: graceful degradation on store failure (BR-11, restated)
`cmd/opcua-mcp.go` treats `store.Open` failure as non-fatal: log, force
`EnableCache=false`, skip `SubscriptionManager`/`ReconnectWatcher.Start`,
continue serving every live tool. This is the one new resilience pattern
Unit 3 introduces — Units 1/2 always assumed a working store.

## Performance: TTL-based expiry, no eviction/size cap
Per `requirements.md` Q6 (already decided at Requirements Analysis, not
revisited here): `SearchConfig.MaxCacheSize` is removed, not replaced by a
new cap. The `values`/`typeinfo`/`browse` buckets grow with distinct node
IDs touched, bounded in practice by the OPC-UA server's own address-space
size (thousands of nodes, per Units 1/2's scale assumption) — no eviction
policy is needed at that scale, and adding one now would be solving a
problem this project doesn't have yet.

## Concurrency pattern: no new pattern needed
`CachingClient` holds no mutable state of its own beyond its immutable
`client`/`cache`/`cfg`/TTL fields set at construction — every method call
is independently safe via `Store`'s own bbolt-transaction concurrency
(Unit 1) and `Client`'s own `mu`-guarded snapshot (Phase 0/1). No new
locking is introduced.

## Error-handling pattern: cache errors fall through to live, never fail the call
A `GetValue`/`GetBrowse`/`GetTypeInfo` error (as opposed to a clean
"not found") is treated identically to a cache miss — log via
`internal/logger` and proceed to the live path — rather than returning
the store error to the caller. A degraded cache must never turn a
would-have-succeeded live read into a hard failure (NFR-1.2, consistent
with BR-11's broader graceful-degradation stance). A `PutValue`/`PutBrowse`/
`PutTypeInfo` write failure after a successful live call is logged and
swallowed the same way — the live result is still returned to the caller;
only the opportunistic cache population is lost.

## Testability pattern: reuse the existing mock seam, extend `valueTypeBrowseStore` alongside it
`CachingClient`'s tests mock `valueTypeBrowseStore` (a narrow interface
over `*store.Store`, same pattern as `subscriptionStore` in Unit 2) and
`*Client` is used directly with the existing `opcuaClient` mock seam
underneath it (no new mock needed at the `Client` level — `CachingClient`
wraps the real `*Client` type, not an interface, since it needs no
`Client` methods beyond what's already public and stable).
