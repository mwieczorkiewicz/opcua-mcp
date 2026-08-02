# NFR Requirements — Unit 3: Read-Through Caching & MCP Integration

Built in autonomous mode — see `functional-design/domain-entities.md`'s
header note. Items marked **(auto-decided)** replace a `[Answer]:` question.

## Reliability
- **Store-open/write-failure degradation** (BR-11, auto-decided in
  Functional Design): reaffirmed here as the formal NFR-1.2 answer —
  `cmd/opcua-mcp.go` treats a `store.Open` failure as non-fatal, forcing
  `EnableCache=false` for that process lifetime and skipping
  `SubscriptionManager`/`ReconnectWatcher` startup, while every live
  (non-cached, non-subscribed) tool keeps working.
- Shutdown ordering (BR-10) is the other reliability-relevant decision;
  already fully specified, no open question.

## Scalability
Carried over from Units 1/2 (thousands of nodes, small number of distinct
subscription intervals). Unit 3 adds no new scaling dimension — it reads
the same `values`/`typeinfo`/`browse` buckets Units 1/2 already size for.

## Performance
No new throughput target. `STORE_TYPEINFO_TTL`/`STORE_BROWSE_TTL` (Unit 1
defaults: 24h/5m) bound how often a live call is needed; `max_age_ms`
defaulting to `0` means `opcua_read` callers who don't opt in see no
behavior change (BR-2).

## Availability
N/A — carried over from the project-level resiliency-extension answers
(experimental/lab tool).

## Security
- **SECURITY-01** (bbolt at-rest encryption, deferred from
  `requirements.md` to NFR Design — auto-decided in `nfr-design-patterns.md`
  below): **not required**. Rationale in NFR Design.
- **SECURITY-13** (integrity/audit beyond what's there, deferred — also
  auto-decided in NFR Design): bbolt's own single-writer/ACID transaction
  guarantees are sufficient; no additional audit log for cache
  reads/writes is warranted at this scale/criticality.
- **SECURITY-10** (dependency pinning): the new test-only
  `testcontainers-go` dependency is pinned via `go.sum`, same as every
  other dependency; the Microsoft OPC-UA test server image used by the
  integration suite is pinned to the exact tag already in this repo's
  `docker-compose.yml`, **`mcr.microsoft.com/iot/opc-ua-test-server:2.8`**
  (confirmed present, added by concurrent work on the Docker Compose dev
  stack) — reusing the existing pin rather than introducing a second,
  possibly-drifting reference to the same image.
- **SECURITY-15**: `CachingClient`'s store I/O errors are handled
  explicitly per BR-2/3/4/5 (a store read/write error surfaces as a
  fall-through to the live path, never a panic or a silently-wrong cached
  value — see `nfr-design-patterns.md` for the exact fallback rule).

## Tech Stack Selection
- **PBT-09 framework** (requirements.md flagged this as needing explicit
  confirmation): `pgregory.net/rapid` — **confirmed**, continuing what
  Units 1/2 already use. **(auto-decided)**: no reason to introduce
  `gopter` as a second PBT framework partway through the same AIDLC run;
  consistency across units outweighs any per-framework feature
  difference for this project's scale.
- **`github.com/testcontainers/testcontainers-go`** (new, test-only,
  approved in `requirements.md` Q7): used for the integration-test suite's
  container lifecycle (start the pinned OPC-UA test-server image, wait for
  the port to be reachable, tear down after the test). No production code
  depends on it — confined to `_test.go` files behind the `integration`
  build tag (NFR-4.2).

## Maintainability
Same conventions as Units 1/2: `internal/logger` for all logging,
`fmt.Errorf("...: %w", err)` wrapping, table-driven example tests.

## Testability
- **PBT-02** (already satisfied by Unit 1 — `store`'s encode/decode
  round-trip): no new work needed in Unit 3.
- **PBT-03** (requirements.md's other invariant-PBT candidate, the
  cache's TTL-expiry invariant `cached ⟺ within TTL`): Unit 3's concrete
  instance is `CachingClient.Read`'s cache-or-live decision (BR-2) —
  a property test asserts that for any `ValueEntry` with
  `Source == "live"`, `Read` returns `Source: "cache"` **iff**
  `time.Since(ReceivedAt) <= maxAgeMs`, across randomly generated
  `ReceivedAt`/`maxAgeMs` pairs (including boundary/negative-duration
  edge cases). Complements, not replaces, example-based tests for the
  specific BR-2/3/4/5 scenarios (PBT-10).
- **NFR-4.1/4.2** (integration tests, `requirements.md` Q7): a
  `//go:build integration`-tagged test file in a new
  `internal/opcua/integration_test` scope (see NFR Design for exact
  package placement), covering: subscribe → live value change on the real
  test server → `values` bucket updated with `Source: "subscription"`;
  read-through cache hit/miss/expiry against the real server for an
  unsubscribed node. **(auto-decided, scope note)**: the originally-listed
  "forced disconnect/reconnect scenario verifying subscriptions survive"
  is included only if it can be driven deterministically within the test
  (stopping/restarting the test-server container via `testcontainers-go`
  and observing `ReconnectWatcher`'s rebuild) — if container restart
  timing proves too flaky for a reliable CI assertion, it's descoped to a
  documented manual verification step instead of a flaky automated test,
  consistent with this project's "reliable local baseline" principle
  (`CLAUDE.md`). Which of the two outcomes was actually implemented is
  recorded in the Unit 3 code generation summary, not decided here.
- A separate `make test-integration` target (or equivalent
  `go test -tags=integration ./...`) keeps `go test ./...`/`make test`
  Docker-free and fast, per NFR-4.2.
