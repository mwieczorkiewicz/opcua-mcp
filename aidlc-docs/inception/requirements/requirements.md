# Requirements — Phase 2: Subscriptions + Persistent Cache

## Intent Analysis Summary

- **User Request**: Implement Phase 2 of the opcua-mcp hardening/enhancement
  effort — push-based OPC-UA subscriptions and a persistent (bbolt-backed)
  cache for values, node type info, and browse results — following this
  repo's AIDLC workflow, with an explicit prerequisite gopcua v0.8.0→v0.9.0
  upgrade (verified zero-risk, already completed as commit `7db8353`).
- **Request Type**: New Feature (subscriptions, persistent cache, 3 new MCP
  tools) + Enhancement (read-through caching added to existing `opcua_read`).
- **Initial Scope Estimate**: Multiple Components — new `internal/store`
  package, new `internal/opcua/subscription.go`, changes to
  `internal/opcua/client.go`, `internal/opcua/discovery.go`,
  `internal/mcp/server.go`, `internal/config/config.go`, plus new
  integration-test infrastructure and Docker/README updates.
- **Initial Complexity Estimate**: Complex — new concurrency (reconnect
  handling, though substantially simplified by the v0.9.0 upgrade's verified
  behavior), a new persistence layer, and end-to-end integration testing
  against a real OPC-UA server via testcontainers.
- **Depth**: Comprehensive — production-adjacent (bridges to live industrial
  equipment elsewhere in this codebase), multiple architectural decisions,
  now-resolved via 20 clarifying questions across two rounds (13 main + 7
  resiliency-extension).

## Scope Decisions (from clarifying questions — see `requirement-verification-questions.md` and `resiliency-extension-questions.md` for full Q&A)

- **Full scope, single pass** (Q1=A): subscriptions and the persistent cache
  ship together, not split into separate passes.
- **Subscriptions persist across process restart** (Q2=A): the `subscriptions`
  bucket stores intent (node IDs + interval); on startup, persisted
  subscriptions are automatically re-established — not just resilient to a
  live reconnect (which gopcua v0.9.0 already handles internally), but to a
  full process restart too.
- **Write-through cache invalidation** (Q3=A): a successful `opcua_write`
  immediately invalidates that node's `values`-bucket entry if it's only
  read-through cached (not subscribed) — closes the staleness window for a
  write-then-read-back pattern.
- **Config defaults** (Q4=B): use the proposed `STORE_*` defaults, except
  `STORE_DB_PATH` default is **`mcp_opcua_store.db`** (not
  `opcua_store.db`).
- **bbolt version** (Q5=B): bundle a bump to the latest v1.5.x while
  promoting to a direct dependency (was v1.4.0, indirect).
- **Dead config cleanup** (Q6=A): `SearchConfig.EnableCache` becomes the real
  master toggle for read-through caching (false → always live); the
  still-unused `CacheTTL`/`MaxCacheSize` fields are removed.
- **Verification approach** (Q7=C): in addition to mocked unit tests,
  implement real end-to-end/integration tests against the Microsoft OPC-UA
  test server, using testcontainers-go if reasonable. **New test-only
  dependency approved**: `testcontainers-go` (plus its Docker-module
  dependency) — this is a test-scope addition, distinct from the "no new
  direct [production] dependencies without sign-off" rule, and is
  explicitly approved by this answer.
- **Tool naming** (Q8=A): `opcua_subscribe`, `opcua_unsubscribe`,
  `opcua_list_subscriptions`, as originally proposed.
- **No `opcua_read` backward-compatibility constraint** (Q9=C): the response
  shape may change as needed for the new `max_age_ms`/`source`/`cached_at`
  fields — no client depends on the current exact shape yet.
- **Docker/deployment updates in scope** (Q10=A): `Dockerfile`/`README.md`
  updated to document/mount the new bbolt DB path, same treatment as
  `search_index/` today.
- **Extensions enabled** (Q11/Q12/Q13=A): resiliency baseline, security
  baseline, and property-based testing (full enforcement) all apply to this
  work (see Extension Compliance below).

### Resiliency extension — deployment-context answers

This is an **experimental/lab-deployed tool** (user's own words, Q5 of the
resiliency extension round), not a managed multi-region production service.
Accordingly:
- **RTO/RPO** (Q1=E, N/A): the bbolt cache is derived/re-derivable data (live
  values are re-readable from the OPC-UA server; subscription intent could
  be manually recreated) — no formal recovery targets apply.
- **Change management** (Q2=C, N/A): no formal process for this project.
- **CI/CD** (Q3=A): **existing pipeline identified**:
  `.github/workflows/docker-build.yml` — "Build and Push Docker Image",
  builds multi-arch (`linux/amd64,linux/arm64`) Docker images on push to
  `main`/`develop` and on PRs to `main`, pushes to `ghcr.io`. (The user's
  answer named no pipeline explicitly; this was resolved by finding the
  workflow file already in the repo rather than asking again — flagging
  here for correction if wrong.)
- **Rollback** (Q4=E, N/A): no formal rollback mechanism.
- **Deployment style** (Q5, Other): "Experimental, used mostly in a
  laboratory setting" — informs the whole resiliency posture below.
- **Regional topology** (Q6, Other, no description given): **inferred as
  N/A** (single process/container, no cloud regions) from the Q5 lab/
  experimental context — **flagging prominently for correction at this
  document's review gate if this inference is wrong.**
- **Incident response** (Q7=C, N/A): no formal process.

## Functional Requirements

### FR-1: Persistent store (`internal/store`)
- FR-1.1: A new `internal/store` package wraps `go.etcd.io/bbolt` (promoted
  to a direct dependency, bumped to the latest v1.5.x per Q5).
- FR-1.2: `store.Open(path string, timeout time.Duration) (*Store, error)`
  using `bbolt.Open(path, 0600, &bbolt.Options{Timeout: timeout})`. Timeout
  MUST be non-zero by default (`STORE_OPEN_TIMEOUT`, default `5s`) — a
  zero/unset timeout causes `bbolt.Open` to hang indefinitely on a stale
  lock from a prior ungraceful shutdown (verified against bbolt v1.4.0
  source during Phase 2 planning; must re-verify against whatever v1.5.x
  patch lands, though `Options.Timeout`'s behavior is a stable, old API).
- FR-1.3: Five buckets created via `CreateBucketIfNotExists` on open:
  `values`, `typeinfo`, `browse`, `nodes`, `subscriptions`.
- FR-1.4: Typed `Get`/`Put`/`Delete`/`List` helpers per bucket, JSON-encoded
  values. A `Get` on a missing key returns a typed "not found" error, not a
  panic or a bare nil.
- FR-1.5: `Store.Close() error` — idempotent-safe to call once during
  shutdown; called exactly once, strictly last in the shutdown sequence
  (after `SubscriptionManager`/`DiscoveryService` have both stopped).

### FR-2: Subscription management (`internal/opcua/subscription.go`)
- FR-2.1: `SubscriptionManager` wraps `Client.Subscribe`/`Subscription.Monitor`
  (gopcua v0.9.0, verified byte-for-byte-compatible signatures with v0.8.0).
  One shared `notifyCh chan *ua.PublishNotificationData` for all logical
  subscriptions; `PublishNotificationData.SubscriptionID` disambiguates
  which logical subscription a notification belongs to.
- FR-2.2: A pump goroutine micro-batches incoming notifications into
  `values`-bucket writes within `STORE_BATCH_WINDOW` (default `25ms`) /
  `STORE_BATCH_MAX_ITEMS` (default `250`).
- FR-2.3: `Subscribe(ctx, nodeIDs []string, intervalMs int) (subscriptionID, error)`,
  `Unsubscribe(ctx, subscriptionID or nodeIDs) error`,
  `ListSubscriptions() ([]SubscriptionInfo, error)`.
- FR-2.4: The `subscriptions` bucket persists **intent only** (node IDs +
  publishing interval) — never server-assigned ephemeral `SubscriptionID`s,
  which are meaningless after a reconnect or restart (confirmed via v0.9.0
  source: `SubscriptionID` can change value across a transfer/recreate even
  within a single process's lifetime).
- FR-2.5 (per Q2=A): **On startup**, after `store` opens and before serving
  MCP requests, `SubscriptionManager` reads all persisted intent from the
  `subscriptions` bucket and re-issues `Subscribe`/`Monitor` for each,
  re-establishing subscriptions that existed before a restart.
- FR-2.6: **Reconnect handling** follows the v0.9.0-verified model — do
  **not** poll `Client.State()` on a ticker and eagerly resubscribe on every
  transient blip (fighting gopcua's own internal reconnect/transfer logic).
  Instead, use `opcua.StateChangedCh` (or `StateChangedFunc`) to watch
  specifically for the transition into `Closed` after having been
  connected — gopcua's own `AutoReconnect`/`monitor` goroutine already keeps
  existing `*Subscription` objects (and their `Notifs` wiring) working
  across ordinary reconnects. Only on a confirmed `Closed` (permanent,
  unrecoverable client death) does `SubscriptionManager` construct a new
  `*opcua.Client`, reconnect, and re-issue `Subscribe`/`Monitor` from
  persisted intent (same code path as FR-2.5's startup warm-start).

### FR-3: Read-through caching
- FR-3.1: `opcua_read` gains an optional `max_age_ms` parameter (default
  `0`). Subscribed nodes are always served from the `values` bucket
  (freshest available, since the subscription pump keeps it current),
  unconditional on `max_age_ms`.
- FR-3.2: Unsubscribed nodes are served from the `values` bucket if
  `time.Since(ReceivedAt) <= max_age_ms`, else read live via the existing
  batched `Client.Read` call and opportunistically cached.
- FR-3.3: Response gains `source` (`"live"|"cache"|"subscription"`) and
  `cached_at` fields. Per Q9=C, no constraint that this be byte-identical to
  today's shape when `max_age_ms` is omitted — implement whatever shape is
  cleanest.
- FR-3.4: `opcua_browse_nodes` (and/or `opcua_browse`) checks the `browse`
  bucket first (keyed by parent node ID, `STORE_BROWSE_TTL` default `5m`);
  on hit within TTL, skips the live `Browse` call.
- FR-3.5: `GetNodeTypeInfo` checks the `typeinfo` bucket first
  (`STORE_TYPEINFO_TTL` default `24h`); on hit, zero live reads; on miss/
  expiry, performs the existing 5 reads and caches the result. This is the
  full fix for the H4 finding (P1-4 already removed the redundant
  in-request duplicate fetch; this removes the redundant work across
  separate `Write()` calls to the same node over time).
- FR-3.6 (per Q3=A): a successful `opcua_write` immediately deletes that
  node's `values`-bucket entry if it isn't subscribed (closing the
  write-then-read-back staleness window). Subscribed nodes don't need this —
  the next push naturally corrects them.
- FR-3.7 (per Q6=A): `SearchConfig.EnableCache` (`SEARCH_ENABLE_CACHE`)
  becomes the master on/off switch for FR-3.1–FR-3.6's read-through
  behavior — when `false`, `opcua_read`/browse always go live, matching
  today's behavior exactly. `CacheTTL`/`MaxCacheSize` fields are removed
  (superseded by the `STORE_*` TTL config).

### FR-4: New MCP tools
- FR-4.1: `opcua_subscribe(node_ids, interval_ms)` — subscribes to one or
  more nodes.
- FR-4.2: `opcua_unsubscribe(node_ids)` or `(subscription_id)` — cancels a
  subscription.
- FR-4.3: `opcua_list_subscriptions()` — returns all active subscriptions
  (from persisted intent, reflecting reality even immediately after a
  restart before the warm-start re-subscribe completes, per FR-2.5).
- FR-4.4: Tool descriptions must be distinct enough to avoid reintroducing
  the tool-overlap issue already present for `opcua_read`/`opcua_get_value`
  and `opcua_browse`/`opcua_browse_nodes` (documented as technical debt in
  `code-quality-assessment.md`) — not fixing that existing overlap in this
  pass, just not adding new instances of it.

### FR-5: Configuration
- FR-5.1: New `StoreConfig` struct (`envPrefix "STORE_"`): `DBPath`
  (default `mcp_opcua_store.db`, per Q4), `OpenTimeout` (default `5s`),
  `TypeInfoTTL` (default `24h`), `BrowseTTL` (default `5m`), `BatchWindow`
  (default `25ms`), `BatchMaxItems` (default `250`),
  `NotifyChanBuffer` (default `1024`).
- FR-5.2: `SearchConfig.EnableCache` repurposed per FR-3.7; `CacheTTL`/
  `MaxCacheSize` fields removed.

## Non-Functional Requirements

### NFR-1: Reliability / Resiliency (resiliency baseline extension — see Extension Compliance below)
- NFR-1.1 (RESILIENCY-10, applicable): all external calls in the new code
  (bbolt I/O, OPC-UA Subscribe/Monitor/Read/Write calls) MUST use explicit,
  bounded contexts/timeouts — no unbounded waits. `bbolt.Open`'s
  `Options.Timeout` (FR-1.2) is the concrete instance of this for store
  startup.
- NFR-1.2 (RESILIENCY-10, applicable): graceful degradation — if the store
  fails to open or a bbolt write fails, the server MUST still serve live
  (non-cached, non-subscribed) reads/writes/browses; caching/subscriptions
  are an enhancement, not a hard dependency for basic operation.
- NFR-1.3: shutdown ordering is strict: `SubscriptionManager` and
  `DiscoveryService` both fully stop (joining their goroutines) before
  `store.Close()` — a write-after-close from an in-flight batch must never
  happen (verified via a goroutine-count + no-error-logged test, matching
  the existing `internal/mcp/server_test.go` shutdown-test style).
- NFR-1.4: all other resiliency-baseline rules (RESILIENCY-01 through 09,
  11 through 15) are **N/A** for this pass — see Extension Compliance.

### NFR-2: Security (security baseline extension)
- NFR-2.1 (SECURITY-03, applicable): all new logging goes through
  `internal/logger`, no secrets/PII in log output (the bbolt file path and
  node IDs are not secrets).
- NFR-2.2 (SECURITY-10, applicable): `testcontainers-go` and any new direct
  dependencies (bbolt bump) are pinned via `go.sum`; no unpinned/`latest`
  image tags introduced for the Microsoft OPC-UA test-server image used in
  integration tests (pin a specific tag/digest).
- NFR-2.3 (SECURITY-15, applicable): all new external calls (bbolt, OPC-UA)
  have explicit error handling; resources (bbolt transactions, OPC-UA
  subscriptions) are cleaned up on error paths.
- NFR-2.4: rules requiring web-facing concerns (SECURITY-01 encryption at
  rest for the bbolt file, SECURITY-04 HTTP headers, SECURITY-05 API input
  validation beyond what MCP tool handlers already do, SECURITY-06/07/08
  IAM/network/authz, SECURITY-09 hardening beyond the existing Dockerfile
  gaps already tracked as technical debt, SECURITY-11 through 14) are
  evaluated per-rule at Application Design/NFR Design time, not decided
  here — flagged as likely N/A or already-tracked given this project has no
  web-facing HTML, no user authentication, and no IAM surface.

### NFR-3: Testability (property-based testing extension, full enforcement)
- NFR-3.1 (PBT-02, applicable): the `store` package's JSON encode/decode
  round-trip (`Put` then `Get` for each of the 5 bucket value types) MUST
  have a property-based round-trip test.
- NFR-3.2 (PBT-03, applicable): `NodeInfo`'s mark-and-sweep invariant
  (`SeenInGen` bookkeeping) and the read-through cache's TTL-expiry
  invariant (`cached ⟺ within TTL`) are candidates for invariant PBTs.
- NFR-3.3 (PBT-06, applicable): `SubscriptionManager` is a stateful
  component (subscribe/unsubscribe/notify sequences) — a strong candidate
  for stateful PBT (random command sequences against a simplified reference
  model of "what subscriptions should exist").
- NFR-3.4 (PBT-09): framework selection for Go — `github.com/leanovate/gopter`
  or `pgregory.net/rapid` are the two realistic options; **Application
  Design / NFR Requirements should confirm the specific choice** (not
  decided here, since this is itself a new-dependency decision requiring
  its own explicit sign-off per this project's hard rules).
- NFR-3.5 (PBT-10): PBT complements, does not replace, the existing
  example-based/table-driven test style — both must exist for
  business-critical paths (e.g., subscription reconnect behavior, write
  validation).

### NFR-4: Verification / Integration Testing (per Q7=C)
- NFR-4.1: real end-to-end tests against the Microsoft OPC-UA test server
  (`mcr.microsoft.com/iotedge/opc-plc`, already referenced in `README.md`'s
  dev-environment docs and `Makefile`'s `start-opcua-server` target),
  orchestrated via `testcontainers-go` if reasonable, covering at minimum:
  subscribe → receive live value change → verify `values` bucket updated;
  a forced disconnect/reconnect scenario verifying subscriptions survive;
  read-through cache hit/miss/expiry against a real server.
- NFR-4.2: these integration tests MUST be separated from the default
  `go test ./...`/`make test` run (e.g. a Go build tag like
  `//go:build integration` and a dedicated `make test-integration` target)
  so local/CI runs without Docker available stay fast and hermetic, matching
  this project's existing "reliable local baseline" principle
  (`go vet`/`gofmt`/`go test` always green without extra infrastructure).
  This wasn't explicitly asked as a clarifying question (the answer seemed
  unambiguous given Go/testcontainers convention) — flagging here for
  correction if a different tagging/target convention is preferred.

### NFR-5: Deployment (per Q10=A)
- NFR-5.1: `Dockerfile`/`README.md` document a writable, ideally
  volume-mounted path for the new bbolt DB file (`STORE_DB_PATH`), same
  treatment as `search_index/` today.

## Extension Compliance Summary

### Resiliency Baseline
| Rule | Status | Notes |
|---|---|---|
| RESILIENCY-01 | N/A | No formal criticality classification process for this experimental/lab tool |
| RESILIENCY-02 | N/A | User: derived/re-derivable data, no formal RTO/RPO (Q1=E) |
| RESILIENCY-03 | N/A | User: exempt from change management (Q2=C) |
| RESILIENCY-04 | N/A | User: existing GitHub Actions pipeline noted for context (Q3=A), but no formal rollback/deployment-style requirement (Q4=E, Q5=lab/experimental) |
| RESILIENCY-05 | N/A | No centralized cloud observability platform in scope |
| RESILIENCY-06 | N/A | No load balancer / service discovery in this architecture |
| RESILIENCY-07 | N/A | No resiliency-assessment tooling in scope |
| RESILIENCY-08 | N/A | Single process/container, no cloud regions (inferred from Q6, lab/experimental context) |
| RESILIENCY-09 | N/A | No auto-scaling — single instance |
| RESILIENCY-10 | **Applicable** | See NFR-1.1/1.2 |
| RESILIENCY-11/12/13 | N/A | No DR strategy needed per Q1=E |
| RESILIENCY-14 | Deferred | Ask at NFR Design per the rule's own instruction, not Requirements |
| RESILIENCY-15 | N/A | User: no incident response process (Q7=C) |

### Security Baseline
| Rule | Status | Notes |
|---|---|---|
| SECURITY-01 | Deferred to NFR Design | Whether the bbolt file needs at-rest encryption is a real question (industrial process data could be sensitive) — not decided here |
| SECURITY-02 | N/A | No load balancer/API gateway/CDN in this architecture |
| SECURITY-03 | **Applicable** | See NFR-2.1 |
| SECURITY-04 | N/A | No HTML-serving endpoints |
| SECURITY-05 | Already covered | Existing MCP tool handlers already validate required args; new tools follow the same pattern |
| SECURITY-06/07/08 | N/A | No IAM, network ACLs, or multi-user authz in this architecture |
| SECURITY-09 | Partially applicable | Docker hardening gaps already tracked as technical debt (`code-quality-assessment.md`) — out of this pass's scope unless Phase 3 lands first |
| SECURITY-10 | **Applicable** | See NFR-2.2 |
| SECURITY-11 | Deferred to Application Design | Whether subscription/cache logic needs its own dedicated sub-module (already the plan: `internal/opcua/subscription.go`, `internal/store`) |
| SECURITY-12 | N/A | No user authentication in this project |
| SECURITY-13 | Deferred to NFR Design | Whether bbolt-stored data needs integrity/audit beyond what's already there |
| SECURITY-14 | N/A | No security-event alerting infrastructure in scope |
| SECURITY-15 | **Applicable** | See NFR-2.3 |

### Property-Based Testing
| Rule | Status | Notes |
|---|---|---|
| PBT-01 | Deferred to Functional Design (per-unit) | Property identification happens there |
| PBT-02 | **Applicable** | See NFR-3.1 |
| PBT-03 | **Applicable** | See NFR-3.2 |
| PBT-04 | N/A (so far) | No operations claiming idempotency identified yet — revisit at Functional Design |
| PBT-05 | N/A | No oracle/reference implementation exists for this new code |
| PBT-06 | **Applicable** | See NFR-3.3 |
| PBT-07/08 | Deferred to Code Generation | Generator quality / shrinking apply once tests are written |
| PBT-09 | **Applicable, framework TBD** | See NFR-3.4 — needs its own sign-off |
| PBT-10 | **Applicable** | See NFR-3.5 |

## Summary

Phase 2 adds push-based OPC-UA subscriptions (persisted across restarts,
reconnect-resilient via gopcua v0.9.0's verified internal behavior) and a
persistent bbolt-backed cache (values, node type info, browse results) to
opcua-mcp, exposed via 3 new MCP tools and an enhanced `opcua_read`. This is
an experimental/lab-deployed tool, not a managed production service, which
substantially narrows the resiliency/security extension's applicable
surface to dependency-isolation (timeouts, graceful degradation), logging
hygiene, dependency pinning, and error handling/resource cleanup — the
parts of those baselines that are about code quality rather than cloud
operations. Verification includes both mocked unit tests (following this
project's existing `opcuaClient` interface-seam pattern) and real
integration tests against the Microsoft OPC-UA test server, plus
property-based tests for the store's serialization round-trips and the
subscription manager's stateful behavior.
