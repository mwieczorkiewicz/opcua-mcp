# Implementation Plan — opcua-mcp

Ordered, phased todo list. Each item is independently executable and verifiable. Item IDs (`P0-1`, etc.) are the unit of work — a future session should pick items off this list one at a time, in dependency order within and across phases, and check them off against their acceptance criteria. Every item links back to a finding ID from `findings.md` or a feature ID from the F1/F2 design. See `decisions.md` for open questions some items are gated on.

Scope decisions already locked (see `decisions.md` for the full record): H1 is fixed by forcing stderr in stdio mode (not just changing the default); H11 is fixed by removing the two redundant tools outright and gating the two diagnostic tools behind an opt-in flag (breaking change, intentional); only the retracted `caarlos0/env` dependency is upgraded in this plan — `gopcua`/`mcp-go` upgrades are out of scope.

---

## Phase 0 — Critical correctness fixes

Small, foundational, unblock later phases. No new dependencies except P0-5.

### P0-1: Add `sync.RWMutex` to `Client`
- **Goal / rationale**: `Client.client`/`Client.connected` have zero synchronization (H2, critical). This is also the hard prerequisite for Phase 2's `SubscriptionManager`, which is the first code to touch `Client` from a second goroutine.
- **Files**: `internal/opcua/client.go`
- **Change**: add `mu sync.RWMutex` guarding only `client`/`connected` (not `config`, which is immutable post-construction). Add a `snapshot() (*opcua.Client, bool)` helper taking `RLock`. Every existing method that reads `c.client`/`c.connected` directly is changed to call `snapshot()` once at the top and use the locals thereafter. `Connect()` keeps its slow dial/retry loop against a local variable and only takes `mu.Lock()` briefly at the end to publish the result — do not hold the lock across the retry loop, or every concurrent `IsConnected()`/`Read()` call blocks for the full retry duration. `Disconnect()` takes `mu.Lock()` for its short body.
- **Acceptance criteria**: no direct field access to `c.client`/`c.connected` outside `snapshot()`, `Connect()`, and `Disconnect()` (verify via `grep -n "c\.client\b\|c\.connected\b" internal/opcua/client.go`). `go build ./...` and existing 45 tests still pass.
- **Verification**: add a test to `client_test.go` that calls `IsConnected()`/`Read()` in one goroutine while `SetConnectedForTesting()` toggles state in another, run under `go test -race ./internal/opcua/...` — must report no races.
- **Dependencies**: none.
- **Size**: S. **Risk**: must not regress `Connect()`'s retry latency by holding the lock too long.

### P0-2: Force stderr in stdio mode
- **Goal / rationale**: fixes H1 (critical) per the locked decision — force stderr whenever `SERVER_TRANSPORT=stdio` regardless of `SERVER_LOG_OUTPUT`, warning if the user explicitly set `stdout`.
- **Files**: `internal/logger/logger.go`, `cmd/opcua-mcp.go`
- **Change**: in `logger.Init`/`logger.New`, if `cfg.Transport == "stdio"`, force the output writer to `os.Stderr` unconditionally. Detect an explicit user override (`os.LookupEnv("SERVER_LOG_OUTPUT")` returns `"stdout"` with `ok==true`) before `logger.Init` runs and, if so, log one warning line to stderr: `"SERVER_LOG_OUTPUT=stdout is ignored in stdio transport mode; logging to stderr to protect the JSON-RPC stream"`.
- **Acceptance criteria**: with `SERVER_TRANSPORT=stdio` and `SERVER_LOG_OUTPUT=stdout` set, no log bytes appear on stdout; a warning appears on stderr. With `SERVER_TRANSPORT=http`, `SERVER_LOG_OUTPUT=stdout` behaves as today (unaffected).
- **Verification**: `SERVER_TRANSPORT=stdio SERVER_LOG_OUTPUT=stdout go run ./cmd/opcua-mcp 1>/tmp/out.log 2>/tmp/err.log &`, send a minimal JSON-RPC `initialize` request on stdin, confirm `/tmp/out.log` contains only valid JSON-RPC frames and `/tmp/err.log` contains the log lines + the override warning. Add a table-driven unit test in `internal/logger` for the output-resolution function in isolation (transport × configured-output × explicit-override-flag → resolved writer).
- **Dependencies**: none.
- **Size**: S. **Risk**: low; must not accidentally also suppress the warning itself onto stdout.

### P0-3: Fix `GetNodeClass` type assertion
- **Goal / rationale**: H10a — the NodeClass attribute decodes as `int32` on the wire, but the code asserts to `ua.NodeClass` (`uint32`-based), so the assertion always fails against a real server. Confirmed independently against the gopcua v0.8.0 source (`ua/variant.go:265-266`, `ua/enums_gen.go:966`).
- **Files**: `internal/opcua/client.go` (`GetNodeClass`, ~line 401-405)
- **Change**: assert to `int32` first, reject negative values, convert via `ua.NodeClass(uint32(raw))`.
- **Acceptance criteria**: `GetNodeClass` returns the correct `ua.NodeClass` for a canned/mocked `int32`-valued read result; returns an error for a negative or out-of-range value.
- **Verification**: extract the decode logic into a small pure function if not already isolated, and add table-driven unit tests covering valid class values, zero, and negative input. `go test ./internal/opcua/...`.
- **Dependencies**: none.
- **Size**: S. **Risk**: low.

### P0-4: Rewrite `parseStringTo{Int64,UInt64,Float64}` with `strconv`
- **Goal / rationale**: H10b — current `fmt.Sscanf`-based parsing silently accepts trailing garbage (`"12abc"` → `12`, no error), risking a wrong value being written to a live device.
- **Files**: `internal/opcua/client.go` (~line 1595-1641)
- **Change**: replace with `strconv.ParseInt(s, 10, 64)` / `strconv.ParseUint(s, 10, 64)` / `strconv.ParseFloat(s, 64)`, after `strings.TrimSpace`.
- **Acceptance criteria**: `"12abc"`, `"  "`, `""`, `"12.5"` (for the int parsers) all return errors; `"42"`, `" 42 "`, `"-7"` (int64/float only), `"3.14"` (float only) all succeed with the correct value.
- **Verification**: table-driven unit test covering the cases above for all three functions. `go test ./internal/opcua/...`.
- **Dependencies**: none.
- **Size**: S. **Risk**: low.

### P0-5: Bump `caarlos0/env` to v11.4.1
- **Goal / rationale**: `caarlos0/env v11.0.1` (current) is a retracted module version (confirmed via `go list -m -u all`).
- **Files**: `go.mod`, `go.sum`
- **Change**: `go get github.com/caarlos0/env/v11@v11.4.1 && go mod tidy`. Before bumping, skim the module's CHANGELOG (v11.0.1→v11.4.1) for any default-value or parsing-semantic changes that could affect `internal/config/config_test.go`.
- **Acceptance criteria**: `go list -m -u all` no longer flags `caarlos0/env` as retracted or having an update. `go build ./...` and `go test ./...` (specifically `internal/config`) pass unchanged.
- **Verification**: `go build ./... && go test ./... && go list -m -u all | grep caarlos0`.
- **Dependencies**: none.
- **Size**: S. **Risk**: low; watch for config-parsing behavior drift in the changelog before merging.

### P0-6: `Write()` must propagate `ValidateValueForNode` failures
- **Goal / rationale**: new finding — `Write()` currently logs a warning and proceeds even when validation fails (`client.go:271-275`), allowing invalid values to reach a live device.
- **Files**: `internal/opcua/client.go`
- **Change**: return the validation error from `Write()` instead of logging-and-continuing.
- **Acceptance criteria**: `Write()` returns a non-nil, wrapped (`%w`) error when `ValidateValueForNode` fails, and does not issue the underlying `Write` RPC in that case.
- **Verification**: unit test asserting `Write()` returns an error for a type-mismatched value and that no RPC is attempted (via a call-counting seam if one exists after P1-1/P1-4 land, otherwise assert on the returned error and document the RPC-not-attempted behavior via code review since no mock exists yet at this point in the plan).
- **Dependencies**: none.
- **Size**: S. **Risk**: behavior change — a caller relying on today's lenient (log-only) validation will now get hard errors on invalid writes. Call out in README/changelog as a fix, not a breaking API-shape change (tool signature unchanged, only error behavior).

---

## Phase 1 — Performance & robustness

No new dependencies. Several items here (P1-1, P1-3) need a mockable seam around the gopcua client to be properly unit-testable — introducing that seam is in-scope for whichever of these lands first.

### P1-1: `Client.Read` returns per-node partial results
- **Goal / rationale**: H3 — one bad-status node currently discards the whole batch's good results.
- **Files**: `internal/opcua/client.go`; `internal/mcp/server.go` (`handleRead`, `handleGetValue` — adjust to the new per-node-status return shape)
- **Change**: `Read` no longer errors on a per-node bad status; it returns all `resp.Results` with per-node status preserved. Only transport-level failures (context cancellation, RPC-level error from `c.client.Read` itself) return a top-level error.
- **Acceptance criteria**: given a mixed batch (some `ua.StatusOK`, some not), `Read` returns results for every node with correct per-node status, and no top-level error.
- **Verification**: requires a mockable seam — introduce a minimal interface (e.g. `type opcuaReader interface { Read(ctx, *ua.ReadRequest) (*ua.ReadResponse, error) }`) that `Client` depends on instead of the concrete `*opcua.Client` for this call path, so tests can inject a canned mixed-status response. Add the unit test against the mock. `go test -race ./internal/opcua/...`.
- **Dependencies**: land after P0-1 to avoid rebasing the mutex changes.
- **Size**: M. **Risk**: introducing the mock seam is the main scope risk — keep it minimal (only the methods actually needed), don't over-abstract the whole `*opcua.Client` surface.

### P1-2: `Browse` adds a `BrowseNext` continuation-point loop
- **Goal / rationale**: H5 — `Browse` hardcodes a 1000-reference cap and never calls `BrowseNext` (confirmed available in gopcua v0.8.0, `client.go:1158`), silently truncating large browse results.
- **Files**: `internal/opcua/client.go`
- **Change**: after the initial `Browse` call, loop calling `BrowseNext` while `resp.Results[0].ContinuationPoint` is non-empty, aggregating references. Bound the loop (e.g. by `ctx` deadline and/or a max-iteration safety cap) so a misbehaving server can't cause an unbounded loop.
- **Acceptance criteria**: a node with >1000 references returns all of them across multiple internal round trips; a well-behaved server with <1000 references makes exactly one round trip (no regression).
- **Verification**: unit test against the mock seam from P1-1 (extend it with a `BrowseNext` method) simulating a two-page continuation; manual verification note: test against a local OPC-UA simulation server with a node that has >1000 children if one is available.
- **Dependencies**: benefits from reusing P1-1's mock seam, but independently shippable.
- **Size**: M. **Risk**: infinite-loop risk from a buggy/malicious server — the iteration cap is a required acceptance criterion, not optional.

### P1-3: Discovery mark-and-sweep rewrite
- **Goal / rationale**: fixes H6 (no visited-set, graph re-walked as a tree), H7 (wipe-then-rebuild window), and H8 (Bleve never deletes) together, per the F1/F2 architecture design.
- **Files**: `internal/opcua/discovery.go`
- **Change**: add a `walkGen uint64` counter (protect concurrent `discoverNodes` invocations — ticker vs. `opcua_force_discovery` — with a `discoveryMu sync.Mutex` around the whole walk so only one runs at a time). Remove the wipe at `discovery.go:150-153`. `discoverNodeRecursive` upserts each node in place with `SeenInGen = currentGen` under `cacheMutex.Lock()`, and short-circuits recursion into a node already marked with the current generation (fixes H6's redundant re-walk and gives H7's readers a cache that's never wiped-then-empty). After the walk, a sweep pass under one `cacheMutex.Lock()` acquisition collects and deletes every entry with `SeenInGen != currentGen`, then — outside the lock — extends `updateSearchIndex`'s existing batch to call `batch.Delete(id)` for each swept ID alongside the existing `batch.Index(...)` calls (fixes H8).
- **Acceptance criteria**: a node present in one discovery cycle and absent in the next is removed from `nodeCache` and no longer returned by a Bleve search after the second cycle completes. A concurrent `GetNodeInfo` call issued mid-walk never sees a "not found" result for a node that was valid before the walk started and is still present in the live server.
- **Verification**: new `internal/opcua/discovery_test.go` (currently doesn't exist) using a mock browse seam (reuse/extend P1-1's), running `discoverNodes` twice with a node removed from the mock's tree between runs; assert the node is gone from `nodeCache` and from a `SearchNodes`/Bleve query. Add a concurrency test that calls `GetNodeInfo` in a loop from a second goroutine while `discoverNodes` runs, under `go test -race`.
- **Dependencies**: reuses the mock seam from P1-1 if available; otherwise introduces its own minimal `opcuaBrowser` interface.
- **Size**: L. **Risk**: the bulk of the risk is building adequate test infrastructure without a live server — keep the mock interface narrow (Browse only).

### P1-4: Eliminate redundant `GetNodeTypeInfo` call in `Write()`
- **Goal / rationale**: H4, partial fix (full fix lands in P2-8 with persistent caching). `Write()` currently calls `GetNodeTypeInfo` directly, then `ValidateValueForNode` calls it again internally — 10 redundant reads.
- **Files**: `internal/opcua/client.go`
- **Change**: change `ValidateValueForNode`'s signature to accept an already-fetched `*NodeTypeInfo` parameter instead of fetching it itself; `Write()` fetches once and passes it through.
- **Acceptance criteria**: `GetNodeTypeInfo` (and its 5 underlying attribute reads) is called exactly once per `Write()` call, not twice. Round trips per write drop from 11 to 6 (5 reads + 1 write).
- **Verification**: unit test using a call-counting mock (reuse the P1-1 seam) asserting `GetNodeTypeInfo`'s underlying `Read` is invoked exactly 5 times (not 10) during one `Write()` call.
- **Dependencies**: none strictly, but easiest alongside P1-1's mock infra.
- **Size**: S. **Risk**: low; purely internal refactor, no tool-facing signature change.

### P1-5: Fix `MaxNodesPerBrowse` accounting
- **Goal / rationale**: H10e — the per-node sibling counter incorrectly folds in full recursive subtree size, starving later siblings if an early one has a large subtree.
- **Files**: `internal/opcua/discovery.go`
- **Change**: track "direct children processed at this level" independently from the recursive subtree total; the `break` that enforces `MaxNodesPerBrowse` should only consider direct-child count at the current level, not descendants.
- **Acceptance criteria**: given a synthetic tree where the first of 5 siblings has 9,999 descendants and `MaxNodesPerBrowse=10000`, all 5 siblings are still visited (not just the first).
- **Verification**: unit test with the mock seam constructing exactly this shape; assert all 5 siblings appear in the resulting `nodeCache`.
- **Dependencies**: naturally paired with P1-3 (same function, same file) — implement together if convenient, independently shippable otherwise.
- **Size**: S. **Risk**: low.

### P1-6: `setupGracefulShutdown` closes the HTTP server
- **Goal / rationale**: H10c — in http mode, `os.Exit(0)` fires without draining in-flight requests since no reference to the running `http.Server` is kept.
- **Files**: `internal/mcp/server.go`
- **Change**: store the `http.Server` (or equivalent handle from whatever `httpServer.Start` wraps) on `Server`; in `setupGracefulShutdown`, call its `Shutdown(ctx)` with the existing 5s timeout context before `os.Exit(0)`. No-op in stdio mode (no HTTP server to close).
- **Acceptance criteria**: sending SIGTERM to a running http-mode server with an in-flight request allows that request to complete (or be cancelled at the timeout) before process exit; stdio mode shutdown behavior is unchanged.
- **Verification**: manual test — start in http mode, issue a slow request, send SIGTERM, confirm the response is still delivered (or cleanly cancelled at 5s) and the process exits after. Documented as a manual verification step since signal-handling integration tests are impractical to fully automate here.
- **Dependencies**: none.
- **Size**: S. **Risk**: low.

### P1-7: Baseline tests for `internal/mcp` and `internal/opcua/discovery.go`
- **Goal / rationale**: both packages currently have zero test coverage (~950 and ~1000 lines respectively) — a real gap before Phase 2 adds substantially more logic to both.
- **Files**: `internal/mcp/server_test.go` (new), `internal/opcua/discovery_test.go` (extends the file created in P1-3)
- **Change**: add tests for tool-handler argument parsing/error paths (using a mock `Client`/`DiscoveryService` where needed) and for discovery's cache-stats, search-tier, and browse-name-lookup functions.
- **Acceptance criteria**: `go test ./... -cover` shows non-zero coverage for both packages, targeting a reasonable baseline (not exhaustive) — at minimum, every tool handler's argument-validation/error branch and every discovery search-tier function has at least one test.
- **Verification**: `go test ./... -cover`, inspect per-package coverage output.
- **Dependencies**: P1-1, P1-3 (reuses their mock seams).
- **Size**: L. **Risk**: scope creep — cap to the acceptance criterion above rather than chasing a specific coverage percentage.

---

## Phase 2 — Subscriptions + persistent cache (F1 + F2)

See the F1/F2 architecture summary in the approved plan (design verified against gopcua v0.8.0 source: `Client.Subscribe`, `Subscription.Monitor`, `Client.State()` confirmed to exist; no push-based state-change callback exists in this version, so reconnect detection polls `State()`).

### P2-1: `internal/store` bbolt package
- **Goal / rationale**: foundational persistence layer for F1/F2. Promotes the existing indirect dependency `go.etcd.io/bbolt v1.4.0` to direct — the one pre-approved new-dependency exception.
- **Files**: `internal/store/store.go` (new), `internal/store/types.go` (new: `ValueEntry`, `TypeInfoEntry`, `BrowseEntry`, `NodeEntry`, `SubscriptionIntent`), `internal/config/config.go` (new `StoreConfig`), `go.mod`/`go.sum` (promote bbolt), `internal/mcp/server.go` (open store in `NewServer`, close in shutdown — see P2-9)
- **Change**: `store.Open(path, timeout)` using `bbolt.Open(path, 0600, &bbolt.Options{Timeout: timeout})` — the `Options.Timeout` is mandatory (a stale flock from a prior ungraceful shutdown otherwise hangs `bbolt.Open`, and hence stdio startup, indefinitely). Create all 5 buckets (`values`, `typeinfo`, `browse`, `nodes`, `subscriptions`) via `CreateBucketIfNotExists` on open. Typed `Get`/`Put`/`Delete`/`List` helpers per bucket, JSON-encoded values. `StoreConfig` fields: `DBPath` (`STORE_DB_PATH`, default `./opcua_store.db`), `OpenTimeout` (`STORE_OPEN_TIMEOUT`, default `5s`), `TypeInfoTTL` (`STORE_TYPEINFO_TTL`, default `24h`), `BrowseTTL` (`STORE_BROWSE_TTL`, default `5m`), `BatchWindow` (`STORE_BATCH_WINDOW`, default `25ms`), `BatchMaxItems` (`STORE_BATCH_MAX_ITEMS`, default `250`), `NotifyChanBuffer` (`STORE_NOTIFY_CHAN_BUFFER`, default `1024`).
- **Acceptance criteria**: `store.Open`/`Close` round-trips cleanly; all 5 buckets exist after open; a `Get` on a missing key returns a typed "not found" rather than panicking; `bbolt.Open` with a locked file (simulate by opening twice) returns within `OpenTimeout`, not hanging.
- **Verification**: new `internal/store/store_test.go` covering open/close, bucket creation, and Put/Get/Delete round trips for each of the 5 value types, plus a test that a second `Open` against an already-locked DB file returns an error within `OpenTimeout` (not hanging). `go test ./internal/store/...`.
- **Dependencies**: none.
- **Size**: M. **Risk**: the `Options.Timeout` requirement is the single most important acceptance criterion — a missed default here reintroduces a stdio-hang class of bug.

### P2-2: Persist discovery `nodes`/`browse` buckets + startup warm-load
- **Goal / rationale**: extends P1-3's mark-and-sweep with bbolt persistence, enabling startup warm-load instead of blocking on a full walk (F2).
- **Files**: `internal/opcua/discovery.go`
- **Change**: after each sweep, write surviving/updated nodes to `store`'s `nodes` bucket and delete swept ones, alongside the existing Bleve batch operations. In `DiscoveryService.Start(ctx)`, before spawning `discoveryWorker`, call a synchronous `warmLoad()` that reads `store.ListNodes()` into `nodeCache` (no network I/O) before the ticker-driven full walk proceeds.
- **Acceptance criteria**: after a restart with a pre-populated store, `nodeCache` is non-empty immediately after `Start()` returns, before the first full discovery walk completes.
- **Verification**: test that pre-populates a store, constructs a `DiscoveryService` against it with a slow/blocked mock `Browse`, calls `Start`, and asserts `nodeCache` is populated before the mock's `Browse` call is ever unblocked.
- **Dependencies**: P2-1, P1-3.
- **Size**: M. **Risk**: warm-loaded data may be briefly stale until the first walk completes post-restart — acceptable and expected, document it.

### P2-3: `SubscriptionManager` core
- **Goal / rationale**: F1's engine — push-based value updates via gopcua's native `Subscribe`/`Monitor` API.
- **Files**: `internal/opcua/subscription.go` (new)
- **Change**: implement per the architecture — one shared `notifyCh` for all logical subscriptions (gopcua's `PublishNotificationData.SubscriptionID` disambiguates), one pump goroutine micro-batching `values`-bucket writes within `STORE_BATCH_WINDOW`/`STORE_BATCH_MAX_ITEMS`. `Subscribe`/`Unsubscribe`/`ListSubscriptions` methods; `subscriptions` bucket persists intent (node IDs + interval) only — never server-assigned ephemeral IDs, which are meaningless after any reconnect or restart.
- **Acceptance criteria**: a simulated `*ua.DataChangeNotification` arriving on `notifyCh` results in a corresponding entry in the `values` bucket within `BatchWindow`; `ListSubscriptions` reflects persisted intent even after simulating a manager restart (reload from `store`).
- **Verification**: unit test with a mock `Subscribe`/`Monitor` (new interface seam around the relevant `*opcua.Client` methods) feeding synthetic notifications into `notifyCh`, asserting `store.GetValue` reflects them after the batch window elapses. `go test -race ./internal/opcua/...` for the pump goroutine.
- **Dependencies**: P0-1, P2-1.
- **Size**: L. **Risk**: full correctness (actual OPC-UA wire behavior) is only verified against a real or simulated OPC-UA server — note as a manual integration-test step (e.g. against a local open62541 or Prosys simulation server) in addition to the unit tests.

### P2-4: `reconnectWatcher` + `ResubscribeAll`
- **Goal / rationale**: gopcua's own `AutoReconnect` (default on) handles transient secure-channel errors internally, but v0.8.0 has no push-based state-change callback — this repo must poll `Client.State()` to detect the permanent-death (`abortReconnect`) case and re-establish subscriptions.
- **Files**: `internal/opcua/subscription.go`
- **Change**: a ticker-driven goroutine (5s interval) polling `Client.State()`; on transition to `Closed` after having been connected, calls `Client.Connect(ctx)` and then `ResubscribeAll(ctx)`, which reads `store.ListSubscriptions()` (persisted intent) and re-issues `Subscribe`/`Monitor` for each against the new `*opcua.Client`, rebuilding in-memory handle↔nodeID maps from scratch. Also called from `handleConnect` after a successful manual `opcua_connect`, and once at startup after warm-load.
- **Acceptance criteria**: simulating `Client.State()` transitioning `Connected → Closed → Connected` (via the mock) results in exactly one `ResubscribeAll` call that re-issues `Subscribe`/`Monitor` for every persisted intent.
- **Verification**: unit test driving the mock's `State()` return value through this sequence, asserting `Subscribe`/`Monitor` call counts. Manual integration-test step: kill/restart a local OPC-UA test server mid-session and confirm subscriptions resume.
- **Dependencies**: P2-3.
- **Size**: M. **Risk**: real reconnect timing/edge cases only fully verified via manual integration testing.

### P2-5: New MCP tools — `opcua_subscribe`, `opcua_unsubscribe`, `opcua_list_subscriptions`
- **Goal / rationale**: expose F1 to MCP clients.
- **Files**: `internal/mcp/server.go`
- **Change**: register three tools in `SetupTools()`; thin handlers delegating to `SubscriptionManager`. `opcua_subscribe(node_ids, interval_ms)`, `opcua_unsubscribe(node_ids)` or `(subscription_id)`, `opcua_list_subscriptions()`.
- **Acceptance criteria**: all three tools appear in the tool list; `opcua_subscribe` followed by `opcua_read` (with any `max_age_ms`, once P2-6 lands) on the same node returns `source: "subscription"`.
- **Verification**: handler-level test invoking each with a mock `SubscriptionManager`; manual end-to-end MCP tool-call test once connected to a real/simulated server.
- **Dependencies**: P2-3.
- **Size**: S. **Risk**: low; keep descriptions distinct to avoid reintroducing H11-style overlap.

### P2-6: Read-through `max_age_ms`/`source`/`cached_at` on `opcua_read`
- **Goal / rationale**: F2's core read-through behavior.
- **Files**: `internal/mcp/server.go` (`handleRead`), `internal/opcua/subscription.go` (`SubscriptionManager.Read`)
- **Change**: add optional `max_age_ms` tool param (default `0`). Subscribed nodes are always served from the `values` bucket, unconditional on `max_age_ms`. Unsubscribed nodes are served from cache if `time.Since(ReceivedAt) <= max_age_ms`, else read live via a single batched `Client.Read` call (preserving existing batching) and opportunistically cached. Response gains `source` (`"live"|"cache"|"subscription"`) and `cached_at` (omitted for `"live"`).
- **Acceptance criteria**: a request with `max_age_ms` omitted or `0` produces byte-identical `node_id`/`value`/`status`/`source_timestamp`/`server_timestamp` output to pre-P2-6 behavior, plus the two new additive keys. A mixed request (one subscribed node, one fresh-cache node, one stale-cache node needing a live read) resolves each correctly per its own path.
- **Verification**: unit test covering all three source paths and the mixed-batch case, using the P2-1/P2-3 test infra. Explicit regression test asserting old-shape output when `max_age_ms` is omitted.
- **Dependencies**: P2-3.
- **Size**: M. **Risk**: interacts with H3/P1-1 — once P1-1 lands, a live sub-batch failure must not discard already-resolved cache-hit results; add a test for exactly this mixed-failure case.

### P2-7: Browse read-through caching
- **Goal / rationale**: F2 for browse results — avoid re-browsing unchanged subtrees within a TTL window.
- **Files**: `internal/opcua/client.go` or `discovery.go` (cache layer), `internal/mcp/server.go` (`handleBrowseNodes`)
- **Change**: `opcua_browse_nodes` checks the `browse` bucket first (keyed by parent nodeID, `STORE_BROWSE_TTL` default 5m); on hit within TTL, skips the live `Browse` call; on miss/expiry, browses live and caches the result.
- **Acceptance criteria**: two calls to `opcua_browse_nodes` for the same node within the TTL window result in exactly one live `Browse` call (verified via the mock's call counter).
- **Verification**: unit test asserting the mock `Browse` is invoked once for two in-TTL calls and twice when the second call is made past `BrowseTTL` (simulate via a fake clock or by manipulating stored `CachedAt`).
- **Dependencies**: P2-1.
- **Size**: M. **Risk**: low; independent of the subscription track, can land any time after P2-1.

### P2-8: `typeinfo` bbolt cache in `GetNodeTypeInfo`
- **Goal / rationale**: completes H4's fix (P1-4 removed the duplicate in-request call; this removes the redundant work across separate write calls to the same node).
- **Files**: `internal/opcua/client.go`
- **Change**: `GetNodeTypeInfo` checks the `typeinfo` bucket first (`STORE_TYPEINFO_TTL` default 24h); on hit, returns cached data with zero live reads; on miss/expiry, performs the existing 5 reads and writes the result to the bucket.
- **Acceptance criteria**: two `Write()` calls to the same node within the TTL window result in exactly 5 live type-info reads total (first call only), not 10.
- **Verification**: unit test with a call-counting mock asserting read counts across two sequential `Write()` calls to the same node.
- **Dependencies**: P2-1, P1-4.
- **Size**: M. **Risk**: a node's data type changing on the live server mid-TTL-window would go undetected for up to `TypeInfoTTL` — rare in practice for OPC-UA (data types aren't normally redefined online); document as an accepted tradeoff.

### P2-9: Shutdown/lifecycle hardening integration pass
- **Goal / rationale**: verify the ordering rules from the F1/F2 architecture risk list end-to-end now that all the pieces (store, subscriptions, discovery, HTTP server) exist together.
- **Files**: `internal/mcp/server.go` (`setupGracefulShutdown`), `internal/opcua/subscription.go`, `internal/store/store.go`
- **Change**: ensure shutdown order is: stop `SubscriptionManager` (joins pump + reconnectWatcher via `WaitGroup`) and `DiscoveryService` in parallel or sequence, both completing (within the existing 5s timeout) before `store.Close()`; `store.Close()` called exactly once, strictly last.
- **Acceptance criteria**: after a simulated shutdown, no goroutine started by `SubscriptionManager`/`DiscoveryService` remains running (checked via `runtime.NumGoroutine()` before/after with reasonable tolerance); `store.Close()` is never called while a pump batch write is in flight (no error/panic from a closed-DB write).
- **Verification**: integration test starting all components, invoking the shutdown path directly (not via OS signal, to keep it a normal test), asserting goroutine count returns to baseline and no errors are logged from a write-after-close. Manual test: `SIGTERM` a running local build in both stdio and http modes, confirm clean exit.
- **Dependencies**: P1-6, P2-2, P2-3, P2-4.
- **Size**: M. **Risk**: goroutine-count assertions can be flaky under GC timing — use a retry/tolerance window rather than an exact synchronous check.

---

## Phase 3 — Tool-surface cleanup & docs

Per the locked decision: remove the two redundant tools outright (breaking change, called out explicitly), gate the two diagnostics behind an opt-in flag.

### P3-1: Remove `opcua_get_value`
- **Goal / rationale**: H11 — strict functional subset of `opcua_read`.
- **Files**: `internal/mcp/server.go`, `README.md`
- **Change**: remove the tool registration and `handleGetValue`. Document the removal and `opcua_read` as the replacement in the README's changelog/upgrade-notes section.
- **Acceptance criteria**: `opcua_get_value` no longer appears in the tool list; `go build` succeeds with `handleGetValue` fully removed (not just unregistered, to avoid dead code).
- **Verification**: `grep -rn "opcua_get_value\|handleGetValue" internal/` returns nothing; manual MCP tool-list call confirms the tool is gone.
- **Dependencies**: none, but land alongside P3-2/P3-3 so the breaking-change README note covers all removals at once.
- **Size**: S. **Risk**: breaking change for any existing MCP client wired to this tool name — intentional per the locked decision, must be prominently documented.

### P3-2: Remove `opcua_browse`
- **Goal / rationale**: H11 — strict functional subset of `opcua_browse_nodes` (`max_depth=1`).
- **Files**: `internal/mcp/server.go`, `README.md`
- **Change**: remove the tool registration and `handleBrowse`.
- **Acceptance criteria** / **Verification**: same shape as P3-1.
- **Dependencies**: none; land alongside P3-1/P3-3.
- **Size**: S. **Risk**: breaking change, same as P3-1.

### P3-3: Gate `opcua_debug_search`/`opcua_ensure_server_nodes` behind `MCP_ENABLE_DEBUG_TOOLS`
- **Goal / rationale**: H11 — unguarded internal diagnostics currently exposed model-facing by default.
- **Files**: `internal/config/config.go` (new `MCPConfig.EnableDebugTools`, `env:"MCP_ENABLE_DEBUG_TOOLS" envDefault:"false"`), `internal/mcp/server.go` (conditional registration in `SetupTools`)
- **Change**: only register the two tools when the flag is true.
- **Acceptance criteria**: default (unset) run registers 11 tools (15 − 2 removed in P3-1/P3-2 − 2 gated), `MCP_ENABLE_DEBUG_TOOLS=true` registers 13.
- **Verification**: test asserting `SetupTools()` tool count differs by exactly 2 between the two flag states.
- **Dependencies**: none.
- **Size**: S. **Risk**: low.

### P3-4: Prune/repurpose dead config
- **Goal / rationale**: H9 — `SEARCH_CACHE_TTL`/`SEARCH_MAX_CACHE_SIZE` are dead; `SEARCH_ENABLE_CACHE` gates nothing; `OPCUA_SERVER_CERT` is documented but unimplemented.
- **Files**: `internal/config/config.go`, `internal/opcua/discovery.go` / `subscription.go` (wire `EnableCache` as the master toggle for P2-6/P2-7's read-through behavior), `README.md`
- **Change**: remove `SEARCH_CACHE_TTL`/`SEARCH_MAX_CACHE_SIZE` fields (superseded by `StoreConfig`'s TTLs). Repurpose `SEARCH_ENABLE_CACHE` as the master on/off switch gating P2-6 (`opcua_read` read-through) and P2-7 (browse caching) — when `false`, both fall back to always-live behavior. Resolve `OPCUA_SERVER_CERT` per `decisions.md` item 2 (implement or remove) — this item's completion is gated on that decision.
- **Acceptance criteria**: `config_test.go` covers the new `EnableCache` semantics; no dangling references to the removed fields anywhere in the repo; caarlos0/env does not error at startup on an unrecognized leftover `SEARCH_CACHE_TTL`/`SEARCH_MAX_CACHE_SIZE` env var if a deployment still sets one (verify this before removing — if it does error, treat the removal as a breaking change requiring a README callout, same as P3-1/P3-2).
- **Verification**: `go build ./... && go test ./...`; grep confirms no dangling references; manual check of caarlos0/env's unknown-env-var behavior.
- **Dependencies**: `decisions.md` item 2 (OPCUA_SERVER_CERT resolution); P2-6/P2-7 (for `EnableCache` to have something to gate).
- **Size**: S. **Risk**: possible breaking change if caarlos0/env errors on unrecognized env vars — verify first.

### P3-5: README rewrite
- **Goal / rationale**: H11 + testing-claim finding — README documents only 7 of 15 (pre-cleanup) tools and overstates test coverage.
- **Files**: `README.md`
- **Change**: document every tool remaining after P3-1/P3-2/P3-3 (including the new subscribe/cache tools from P2-5), every `STORE_*`/`MCP_ENABLE_DEBUG_TOOLS` env var, revise the "Comprehensive Testing" claim to match actual coverage post-P1-7, and resolve the `OPCUA_SERVER_CERT` documentation per P3-4's outcome.
- **Acceptance criteria**: every tool name in `SetupTools()` appears in the README tool list; every env var in `config.go` appears in the README's env var tables.
- **Verification**: manual cross-check (or a small script diffing `grep` output from both sources).
- **Dependencies**: P2-5, P3-1, P3-2, P3-3, P3-4.
- **Size**: M. **Risk**: low; do this item last so it reflects the final repo state.

### P3-6: Makefile — add `go vet`
- **Goal / rationale**: `make all`/`make test` never run `go vet` despite the README telling users to run it manually.
- **Files**: `Makefile`
- **Change**: add `go vet ./...` to the `test` (or a new `vet`) target, wired into `all`, failing the build on vet errors.
- **Acceptance criteria**: `make test` runs `go vet ./...` and fails if vet reports an issue.
- **Verification**: run `make test`; temporarily introduce a deliberate vet error, confirm `make test` fails, then revert.
- **Dependencies**: none.
- **Size**: S. **Risk**: low.

### P3-7: Dockerfile hardening
- **Goal / rationale**: no non-root `USER`, no `HEALTHCHECK`, unpinned builder base image.
- **Files**: `Dockerfile`
- **Change**: add a numeric non-root `USER` (per `decisions.md` item 4), add a `HEALTHCHECK`, pin the `cgr.dev/chainguard/go` builder stage to a specific tag or digest.
- **Acceptance criteria**: `docker build .` succeeds; the final image runs as the chosen non-root numeric UID; `docker inspect` shows the `HEALTHCHECK` config; any volume-mounted paths (`search_index/`, the new bbolt DB file) remain writable by that UID.
- **Verification**: `docker build -t opcua-mcp:test . && docker inspect opcua-mcp:test` (check `Config.User` and `Config.Healthcheck`); run the container against a mounted volume and confirm it can write `search_index/` and the bbolt DB file.
- **Dependencies**: `decisions.md` item 4 (UID choice).
- **Size**: S. **Risk**: low; verify write permissions on mounted paths for the chosen UID before finalizing.
