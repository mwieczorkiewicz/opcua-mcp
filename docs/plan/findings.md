# Findings — opcua-mcp Audit

Evidence gathered by independently reading `internal/opcua/` (client.go, discovery.go), `internal/mcp/server.go`, `internal/config/`, `internal/logger/`, `cmd/`, both test files, the Makefile, Dockerfile, and README — plus running `go build/vet/test -race`, inspecting `go.mod`/`go list -m -u all`, and reading the `gopcua@v0.8.0` module-cache source directly for the subscription/browse/NodeClass APIs. Every hypothesis (H1-H11) from the prior review was treated as unverified until checked against this evidence.

Tool baseline (all clean): `go build ./...` succeeds. `go vet ./...` reports nothing. `gofmt -l .` reports nothing. `go test ./...` — 45 tests pass across 5 packages. `go test -race ./...` — same 45 tests pass under the race detector (this does **not** mean H2 is refuted — see below; the race detector only flags races that are actually exercised by a running goroutine pair during the test, and no test currently drives `Client` from two goroutines concurrently). `golangci-lint`/`staticcheck` were not usable in this environment (`golangci-lint`: not installed; `staticcheck`: failed with Go 1.26.5 toolchain/export-format incompatibility) — see `decisions.md` item 6.

---

## H1 — stdio protocol corruption

**Verdict: Confirmed. Severity: Critical.**

- `internal/config/config.go:30` — `Transport` field, `envDefault:"stdio"`.
- `internal/config/config.go:35` — `LogOutput` field, `envDefault:"stdout"`.
- `internal/logger/logger.go:36-52` — `switch cfg.LogOutput { case "stderr": ...; case "file": ...; default: output = os.Stdout }`. The default case (which `"stdout"` falls into) writes to `os.Stdout`.
- `cmd/opcua-mcp.go:21` — `logger.Init(&cfg.Server)` is called at startup, and `logger.New` (`internal/logger/logger.go:73`) does `slog.SetDefault(logger)`, so all `logger.Info/Warn/Error` calls throughout the app go through this stdout-targeting handler by default.
- `internal/mcp/server.go:658-661` — `startStdio()` calls `server.ServeStdio(s.mcpServer)`. In the vendored `mcp-go@v0.39.1` dependency, `ServeStdio` → `Listen(ctx, os.Stdin, os.Stdout)`, which sets the session writer to `os.Stdout`.

**Result**: with default configuration, both the JSON-RPC protocol stream and the application's structured logs are multiplexed onto the same `os.Stdout` file descriptor. Any interleaved log line breaks JSON-RPC framing for any MCP client using stdio transport — which is the default transport. No other stray `fmt.Println`/`fmt.Print*`/`os.Stdout` write sites exist anywhere else in the repo (verified via repo-wide grep) — the bug is entirely in the logger's default-output selection.

---

## H2 — data races and stale connection state

**Verdict: Confirmed. Severity: Critical.**

- `internal/opcua/client.go:19-23` — `Client` struct has fields `client *opcua.Client`, `connected bool`, `config *config.OPCUAConfig`. No `sync.Mutex`/`sync.RWMutex` field exists; `sync` is not imported in client.go at all.
- Unsynchronized read/write sites: `Connect()` writes both fields (client.go:86-87); `Disconnect()` reads then writes `connected` (client.go:117-119); `IsConnected()` reads both (client.go:126-128); every RPC method (`Read` client.go:206+, `Write` client.go:252+, `Browse` client.go:319+, `GetNodeClass` client.go:364+, etc.) calls `IsConnected()` then separately dereferences `c.client` — a classic check-then-use race if any method runs concurrently with `Connect`/`Disconnect`.
- `connected` is never updated on connection loss — no error-code inspection after any RPC call flips it to `false`. A dropped TCP session leaves `connected == true` indefinitely until an explicit `Disconnect()`.
- No keepalive/reconnect logic exists in this repo's code today. (Separately, gopcua's own `*opcua.Client` has internal auto-reconnect behavior by default — see the F1 architecture note below — but this repo's `connected` bookkeeping doesn't track or benefit from that.)
- The discovery worker goroutine (`internal/opcua/discovery.go:95`, `go ds.discoveryWorker(ctx)`) calls `ds.client.IsConnected()`/`Browse` on a timer, concurrently with any MCP tool-handler goroutine using the same `*Client` — this is the concrete concurrent-access scenario the missing mutex fails to guard.
- `go test -race ./...` passes today only because no current test drives two goroutines through `Client` simultaneously — this is a coverage gap, not evidence of safety (see `internal/mcp` / `internal/opcua/discovery.go` test-coverage finding below).

---

## H3 — batch read fails whole batch on one bad status

**Verdict: Confirmed. Severity: High.**

`internal/opcua/client.go:240-245`:
```go
for i, result := range resp.Results {
    if result.Status != ua.StatusOK {
        return nil, fmt.Errorf("read failed for node %s: %s", nodeIDs[i], result.Status)
    }
}
return resp.Results, nil
```
One bad-status node aborts the entire `Read` call, discarding successfully-read values for every other node in the same batch. No per-node partial-result path exists.

---

## H4 — redundant round trips on write

**Verdict: Confirmed. Severity: High.**

`Client.Write` (`client.go:251-315`) call chain:
1. `client.go:263` — direct call to `c.GetNodeTypeInfo(ctx, nodeID)`.
2. `client.go:271` — `c.ValidateValueForNode(ctx, nodeID, value)`, which internally calls `c.GetNodeTypeInfo` **again** (`client.go:805`).
3. `client.go:304` — the actual `Write` RPC.

`GetNodeTypeInfo` (`client.go:754-800`) issues 5 sequential single-attribute `ReadRequest`s, one RPC each: `GetNodeDataType` (client.go:435), `GetNodeValueRank` (client.go:483), `GetNodeArrayDimensions` (client.go:531), `GetNodeAccessLevel` (client.go:580), `GetNodeUserAccessLevel` (client.go:628). None of these are batched into a single multi-attribute `Read` call.

**Total for one `Write()`**: 5 reads (direct `GetNodeTypeInfo`) + 5 reads (inside `ValidateValueForNode`'s internal `GetNodeTypeInfo`) + 1 write RPC = **11 sequential network round trips per node write**.

---

## H5 — Browse ignores continuation points

**Verdict: Confirmed. Severity: Medium.**

`client.go:330-342`:
```go
req := &ua.BrowseRequest{
    RequestedMaxReferencesPerNode: 1000,
    NodesToBrowse: []*ua.BrowseDescription{{
        NodeID: id, BrowseDirection: ua.BrowseDirectionForward,
        ReferenceTypeID: ua.NewNumericNodeID(0, 33), IncludeSubtypes: true,
        NodeClassMask: uint32(ua.NodeClassAll), ResultMask: uint32(ua.BrowseResultMaskAll),
    }},
}
```
No code anywhere in the repo reads `resp.Results[0].ContinuationPoint` or calls `BrowseNext`. Confirmed independently by reading the gopcua v0.8.0 module cache: `func (c *Client) BrowseNext(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error)` exists at `client.go:1158` in the gopcua library — the capability is available upstream and simply unused. Any node with more than 1000 references is silently truncated with no error or indication.

---

## H6 — discovery walks a graph as if it were a tree

**Verdict: Confirmed. Severity: Medium.**

`discoverNodeRecursive` (`discovery.go:183`) has no `visited` set (`grep -n "visited" discovery.go` returns nothing). It recurses unconditionally on every child returned by `Browse` (`discovery.go:215-249`). OPC-UA address spaces are graphs — nodes can be reachable via more than one hierarchical reference (e.g. `Organizes` + `HasComponent`) — so shared subtrees are re-browsed multiple times, wasting RPCs. The depth limit (`MaxDiscoveryDepth`, default 10, `config.go:87`) bounds recursion depth but does not prevent redundant re-traversal of already-seen nodes within that bound.

---

## H7 — cache cleared before rebuild

**Verdict: Confirmed. Severity: Medium.**

`discovery.go:150-153`:
```go
ds.cacheMutex.Lock()
ds.nodeCache = make(map[string]*NodeInfo)
ds.cacheMutex.Unlock()
```
The lock is released immediately after the wipe, not held across the subsequent recursive rebuild (`discoverNodeRecursive`, called at `discovery.go:156`). Any concurrent lookup (`GetNodeInfo`, `GetNodeByBrowseNameFromCache`, `SearchNodes`, etc. — each independently RLocking `cacheMutex`) that runs during the rebuild window sees a partially- or fully-empty cache and returns spurious "not found" results. This runs on a ticker (`discoveryWorker`, `discovery.go:119-140`, default `DISCOVERY_INTERVAL=30s`) concurrently with foreign callers — i.e., this failure window recurs every 30 seconds by default.

---

## H8 — Bleve index never deletes stale documents

**Verdict: Confirmed. Severity: Medium.**

The Bleve index is opened at `cfg.SearchIndexPath` (default `./search_index`, `config.go:92`) — a real on-disk path (`bleve.Open`/`bleve.New`, `discovery.go:39-82`), so it persists across restarts. The only index-mutation call in the repo is `batch.Index(nodeID, nodeInfo)` inside `updateSearchIndex` (`discovery.go:262-284`) → `ds.index.Batch(batch)`. A repo-wide `grep -n "\.Delete("` across all `.go` files returns zero matches. Combined with H7's wholesale in-memory cache wipe on every cycle, nodes removed from the live server remain permanently searchable in Bleve (cross-referenced against the now-empty-then-rebuilt `nodeCache`, so stale hits get silently dropped from results rather than erroring — but the on-disk index itself grows unbounded with dead documents that are never reclaimed).

---

## H9 — dead config fields

**Verdict: Confirmed. Severity: Low.**

Repo-wide grep for each field outside its struct definition and env-parsing:
- `SearchConfig.CacheTTL` (`SEARCH_CACHE_TTL`, `config.go:98`) — zero usages elsewhere. No TTL/eviction logic exists on `nodeCache` (a plain unbounded `map[string]*NodeInfo`).
- `SearchConfig.MaxCacheSize` (`SEARCH_MAX_CACHE_SIZE`, `config.go:99`) — zero usages elsewhere. The node cache is never size-bounded.
- `SearchConfig.EnableCache` (`SEARCH_ENABLE_CACHE`, `config.go:97`) — exactly one usage, purely cosmetic: `discovery.go:833` reports `"cache_enabled": ds.config.EnableCache` in `GetCacheStats()`. It gates no actual behavior.
- `OPCUAConfig.ServerCert` (`OPCUA_SERVER_CERT`, `config.go:51`) — zero usages anywhere else. `Connect`/`addCertificateAuth` (`client.go:92-113`) only reads `CertFile`/`KeyFile`. The README documents `OPCUA_SERVER_CERT` (README.md:91, 189, 246, 489) as if it enables server-certificate pinning/verification; it does not — this is a functional/documentation mismatch, not just an unused-field nit.

---

## H10 — misc smaller issues

**Verdict: Confirmed (all sub-parts). Severity: Mixed (see each).**

**(a) `GetNodeClass` type assertion — High, latent/untested bug.**
`client.go:401-405`:
```go
nodeClass, ok := resp.Results[0].Value.Value().(ua.NodeClass)
if !ok { return 0, fmt.Errorf("failed to extract node class from result") }
```
Verified directly against the gopcua v0.8.0 module source: the NodeClass attribute decodes on the wire via `TypeIDInt32` → `buf.ReadInt32()` (`ua/variant.go:265-266`), producing a plain `int32`. `ua.NodeClass` is defined as `type NodeClass uint32` (`ua/enums_gen.go:966`) — a different concrete type. Go type assertions require exact dynamic-type match, so `ok` is `false` for every real server response — `GetNodeClass` always fails against a live server. `client_test.go` only exercises the "not connected" error branch (lines 169-194), so this is untested and would only surface in production use. Note: `discoverNodeRecursive` does **not** use this buggy method — it reads `NodeClass` off the `Browse` result directly, which decodes correctly — so discovery is unaffected; only direct calls to `GetNodeClass` (used by the `opcua_node_info` tool) are broken.

**(b) `parseStringTo{Int64,UInt64,Float64}` — Medium, correctness/safety.**
`client.go:1595-1641`, e.g.:
```go
func (c *Client) parseStringToInt64(s string) (int64, error) {
    s = strings.TrimSpace(s)
    val, err := fmt.Sscanf(s, "%d", new(int64))
    if err != nil || val != 1 { return 0, fmt.Errorf("invalid integer string: %s", s) }
    var result int64
    _, err = fmt.Sscanf(s, "%d", &result)
    return result, err
}
```
Uses `fmt.Sscanf` (double-call anti-pattern: parses once to validate, again to extract) instead of `strconv`. `fmt.Sscanf` does not require the whole string to be consumed and does not error on trailing unparsed characters — `"12abc"` matches `12` for `%d`, `val==1`, `err==nil`, and the function returns `12` with **no error**. A malformed/truncated numeric input from an MCP tool caller can silently produce a wrong value written to a live OPC-UA device instead of being rejected.

**(c) Graceful shutdown doesn't close the HTTP server — Medium.**
`internal/mcp/server.go:928-952`:
```go
func (s *Server) setupGracefulShutdown() {
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        ...
        if err := s.discovery.Stop(); err != nil { ... }
        if err := s.opcuaClient.Disconnect(ctx); err != nil { ... }
        os.Exit(0)
    }()
}
```
It does stop discovery and disconnect the OPC-UA client, but there's no stored reference to the running HTTP server (`startHTTP()`, server.go:663-672, blocks on `httpServer.Start(":" + port)` with no field on `Server` holding that handle), so in `http` mode `os.Exit(0)` tears down the listening socket immediately without draining in-flight HTTP requests.

**(d) stdio never auto-connects — By design, not a bug, but has a side effect worth noting.**
`cmd/opcua-mcp.go:48-62` explicitly gates the initial `Connect()` call on `cfg.Server.Transport != "stdio"`. In stdio mode, the OPC-UA connection is only established when the model calls `opcua_connect` (`server.go:601-611`). Side effect: the discovery worker starts unconditionally (`server.go:640-644`) and its first tick (`discovery.go:126`) checks `ds.client.IsConnected()` (`discovery.go:144-146`) and errors/no-ops silently until the model connects — every discovery tick before that is a wasted, logged-but-otherwise-silent no-op.

**(e) `MaxNodesPerBrowse` accounting mixes child counts into the per-browse limit — Medium.**
`discovery.go:183-253`:
```go
discovered := 0
for i, ref := range references {
    if discovered >= ds.config.MaxNodesPerBrowse { break }
    ...
    discovered++
    childDiscovered, err := ds.discoverNodeRecursive(ctx, nodeInfo.NodeID, nodeInfo.NodeID, depth+1)
    ...
    discovered += childDiscovered
}
```
`discovered` is meant to bound how many direct children of *this* node get processed, but it's incremented both per direct child (`discovered++`) and by the entire recursive subtree size (`discovered += childDiscovered`). If the first sibling has a large subtree, `discovered` can reach the limit before later siblings are even visited — those get silently dropped via the `break`, even though only one of N direct children was actually processed. The limit is also enforced independently per parent node (each recursive call resets its own local `discovered` to 0), so it does not bound total node count across a discovery run in aggregate.

---

## H11 — tool sprawl

**Verdict: Confirmed. Severity: Medium.**

All 15 tools are registered in `SetupTools()` (`internal/mcp/server.go:57-201`): `opcua_read`, `opcua_write`, `opcua_browse`, `opcua_node_info`, `opcua_server_info`, `opcua_connect`, `opcua_disconnect`, `opcua_get_value`, `opcua_browse_nodes`, `opcua_get_value_by_name`, `opcua_find_similar_nodes`, `opcua_debug_search`, `opcua_discovery_stats`, `opcua_force_discovery`, `opcua_ensure_server_nodes`.

- `opcua_read` (server.go:59-66, `handleRead` at 340-377) vs `opcua_get_value` (server.go:121-128, `handleGetValue` at 674-706): both call `Client.Read` and return the same JSON shape; `opcua_get_value` is a strict single-node subset of `opcua_read` (which already accepts CSV/JSON-array node IDs via `ParseNodeIDs`).
- `opcua_browse` (server.go:83-90, `handleBrowse` at 465-492, one level) vs `opcua_browse_nodes` (server.go:131-144, `handleBrowseNodes` at 708-725, recursive via `browseNodesWithDepth`, server.go:892-926, `max_depth` 1-10 default 3): `opcua_browse` is a strict subset of `opcua_browse_nodes` with `max_depth=1`.
- `opcua_debug_search` (server.go:173-180, `handleDebugSearch` at 812-831 → `discovery.go:884-997` `DebugSearchNodes`) and `opcua_ensure_server_nodes` (server.go:195-198, `handleEnsureServerNodes` at 868-889 → `discovery.go:852-882`) are internal diagnostic/repair tools exposed as unguarded, model-facing MCP tools.

README.md:253-325 documents only 7 of the 15 registered tools — the entire discovery/search subsystem (a major feature) is undocumented in the tools list.

---

## Additional issues found beyond the hypothesis list

- **`Write()` silently ignores validation failures** — `client.go:271-275` logs a warning if `ValidateValueForNode` fails but does not abort the write; an invalid value can still be sent to the live server. (Severity: High — data-integrity risk on a live industrial device.)
- **Nil-pointer panic risk** — `convertValueToOPCUAType`/`validateScalarValue`/`convertScalarValue` call `typeInfo.DataType.IntID()` (e.g. client.go:826, 1063) without checking `typeInfo.DataType` for nil; if `GetNodeDataType` ever returns a nil `*ua.NodeID` with `ok==true`, this panics. (Severity: Medium — no confirmed trigger found, but no guard exists either.)
- **Discovery swallows Browse errors** — `discoverNodeRecursive` (discovery.go:190-204) returns `(0, nil)` on a Browse failure for a non-root node, so a mid-subtree failure is indistinguishable from "node has no children"; `discoverNodes` only surfaces an error if the *root* call itself fails, which it rarely will. Discovery can silently produce an incomplete tree with no signal to any caller. (Severity: Medium.)
- **Zero test coverage** for `internal/mcp` (~950 lines: all 15 tool handlers, resource handlers, `Start`/`startStdio`/`startHTTP`, `setupGracefulShutdown`), `internal/opcua/discovery.go` (~1000 lines: discovery worker, Bleve indexing, all search tiers), `internal/logger`, and `cmd/`. Within `internal/opcua/client.go`, only the "not connected" error branches are tested — no success-path or type-conversion-helper tests exist. Two test files exist total: `internal/config/config_test.go` and `internal/opcua/client_test.go`. (Severity: Medium — directly contradicts the README's "Comprehensive Testing: Unit tests with reasonable coverage" claim, README.md:15.)
- **`caarlos0/env v11.0.1` is a retracted module version** — confirmed via `go list -m -u all`, which flags it `(retracted)`; `v11.4.1` is the latest compatible version. (Severity: Low, easy fix.)
- **Large dependency version gaps** — `gopcua v0.8.0` → `v0.9.0` available; `mark3labs/mcp-go v0.39.1` → `v0.57.0` available (an 18-minor-version jump, likely breaking); `go.etcd.io/bbolt v1.4.0` → `v1.5.0` available (minor). None evaluated for breaking changes in this session — see `decisions.md`.
- **Makefile has no `go vet` target** — `make all` (`clean deps fmt lint test build`) never runs `go vet ./...` even though the README's "Code Quality" section tells users to run it manually. `lint` (golangci-lint) and `security-scan`/`vuln-check` (gosec/govulncheck) all silently no-op if the tool isn't installed, so `make all` can pass in a fresh environment with zero actual linting/security scanning performed.
- **Dockerfile** — multi-stage build (`chainguard/go` → `scratch`) is a genuine strength, but: no `USER` directive (final image runs as root; README.md:677 itself acknowledges non-root is not enforced), no `HEALTHCHECK`, no pinned digest on the `chainguard/go` builder base (rolling tag, non-reproducible builds), no `.dockerignore` (full build context including `.git/` copied into the builder stage — wasteful, not a runtime risk since only the compiled binary reaches the `scratch` stage).
- **No credential logging, no disabled TLS verification found** — repo-wide grep for `Password|Username|Token` in logging calls and for `InsecureSkipVerify|tls.Config` returned no matches; this is a clean bill of health on both fronts.
