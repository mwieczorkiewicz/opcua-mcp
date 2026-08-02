# Unit Dependency Matrix — Phase 2

| Unit | Depends On | Update Priority | Change Scope |
|---|---|---|---|
| Unit 1 — Persistent Store | (none new) | Must-update-first — everything else needs it | Major (new package) |
| Unit 2 — Subscription Management | Unit 1 | Blocks Unit 3 | Major (new files) |
| Unit 3 — Read-Through Caching & MCP Integration | Unit 1, Unit 2 | Last — the integration point | Major (new + modified files) |

## Update Approach
**Sequential** (per Question 2's answer, A) — Unit 1 fully built and tested
before Unit 2 starts; Unit 2 fully built and tested before Unit 3 starts.
This matches the incremental, small-logical-commit discipline this session
has used throughout Phase 0 and Phase 1 (16 commits, each independently
buildable and tested).

## Critical Path
Unit 1 → Unit 2 → Unit 3 (strictly linear — no parallelization opportunity
exists given Question 2's sequential-build answer, and no other unit could
start before Unit 1 regardless, since both Unit 2 and Unit 3 have a hard
compile-time dependency on `store.Store`).

## Coordination Points
- **Unit 1 → Unit 2/3**: the `store.Store` public API (`components.md`'s
  per-bucket typed methods) is the contract — once Unit 1 lands, its method
  signatures shouldn't change without revisiting Units 2/3.
- **Unit 2 → Unit 3**: `SubscriptionManager`'s public API
  (`Subscribe`/`Unsubscribe`/`ListSubscriptions`) plus the "is subscribed"
  provenance signal (`services.md`'s recommendation: carried in
  `store.ValueEntry.Source`, confirmed/finalized during Unit 2's Functional
  Design) is the contract Unit 3's new MCP tools and `CachingClient` build
  against.

## Testing Checkpoints
- After Unit 1: `go test ./internal/store/... -race` green, PBT round-trip
  tests passing, before Unit 2 begins.
- After Unit 2: `go test ./internal/opcua/... -race` green (mocked tests
  only — no live server needed yet), before Unit 3 begins.
- After Unit 3: full `go build/vet/gofmt/test -race ./...` green, plus the
  new integration-test suite passing against the real Microsoft OPC-UA test
  server (testcontainers) — this is the only point at which true end-to-end
  behavior (subscribe → live value change → reconnect → cache hit/miss) is
  verified.

## Rollback Strategy
Each unit lands as its own set of small, independently-revertable commits
(matching this session's established convention) — a failed/problematic
unit can be reverted via `git revert` without necessarily unwinding earlier
units, since Unit 1 has no dependency on 2/3, and Unit 2 has no dependency
on 3.
