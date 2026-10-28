# AI-DLC State Tracking

## Project Information
- **Project**: opcua-mcp
- **Project Type**: Brownfield
- **Start Date**: 2026-08-02
- **Current Stage**: Read-Only Mode + Stale-Session Recovery run COMPLETE (pushed to main). Custom-node bugfix pass committed as 104a989. Next AIDLC phase per CLAUDE.md: OPERATIONS (placeholder).

## Workspace State
- **Existing Code**: Yes
- **Programming Language**: Go (module `github.com/mwieczorkiewicz/opcua-mcp`, `go 1.25.0`)
- **Build System**: Go modules (`go.mod`/`go.sum`), `Makefile`, `Dockerfile`
- **Project Structure**: Single Go module, modular internal packages (`internal/config`, `internal/logger`, `internal/mcp`, `internal/opcua`), one `cmd/` entrypoint
- **Reverse Engineering Needed**: Yes (no prior `aidlc-docs/inception/reverse-engineering/` artifacts exist)
- **Workspace Root**: `/Users/mikolajwieczorkiewicz/software-engineering/opcua-mcp`

## Prior Work (pre-AIDLC, tracked via git log, not this state file)
- Phase 0 (critical fixes) and Phase 1 (performance/robustness), both complete: 16 commits from
  `docs: document commit message convention` (6f9c4c5) through
  `test: baseline coverage for internal/mcp tool handlers and discovery search tiers` (2fbc6e6),
  followed by housekeeping commits (plan-doc removal, README/CLAUDE.md updates) and
  `chore: bump gopcua to v0.9.0` (7db8353). 123 tests passing across 5 packages.
- This was driven by an ad-hoc audit (`docs/plan/{findings,plan,decisions}.md`), now removed since
  every item in it landed. This AIDLC workflow governs all work from here forward instead.

## Scope for This AIDLC Run (superseded - see below for current run)
Phase 2: subscriptions (push-based OPC-UA value updates) + a persistent bbolt-backed cache,
per the approved plan at `/Users/mikolajwieczorkiewicz/.claude/plans/lucky-hugging-blanket.md`.
**Status: COMPLETE** (see Construction Progress below).

## Scope for Current AIDLC Run - Custom-Node Read/Write Correctness Bugfix Pass
Fixing four confirmed root causes (plus one newly-discovered severe bug) behind reports of
null values and "cannot encode" cache errors when reading custom/complex-typed OPC-UA nodes
(observed on the Boiler and ParkingLot sample objects), per the approved plan at
`/Users/mikolajwieczorkiewicz/.claude/plans/i-want-you-to-lucky-candy.md` and
`aidlc-docs/inception/requirements/requirements-custom-node-rw-fix.md`.
**Mode**: Autonomous - user explicitly instructed to proceed following this repo's AIDLC
workflow using sane defaults, without pausing for approval gates or questions unless a
genuine blocker is hit. Same operating rule as "Unit 2 Approved - Autonomous Mode Enabled
for Unit 3" below.
Treated as a single cohesive unit (not decomposed into multiple Units Generation units) since
all fixes touch the same overlapping value-conversion code path, not independent components.

## Scope for Current AIDLC Run - Read-Only Mode + Stale-Session Recovery (started 2026-10-28)
1. Configurable read-only mode with explicit tool allowlist: hidden from tools/list, rejected on direct tools/call.
2. Recovery from StatusBadSessionIDInvalid / StatusBadServerNotConnected: serialized reconnect with the configured security options, one retry; opcua_connect verifies real client health.
**Mode**: Autonomous (Q11 B). Extensions: Security, Resiliency, PBT all enabled (unchanged).

### Stage Progress (this run)
- [x] Workspace Detection - brownfield, reverse engineering artifacts reused
- [x] Requirements Analysis - `requirements-readonly-session-recovery.md`, answers in the questions file
- [x] User Stories - SKIP (single user type, behavior fully specified)
- [x] Workflow Planning - `execution-plan-readonly-session-recovery.md`
- [x] Application Design - SKIP (no new components)
- [x] Units Generation - minimal: read-only-mode, session-recovery
- [x] Construction: read-only-mode - functional design + code, commit bb7fef8
- [x] Construction: session-recovery - functional design + code, commits e655086 (disconnect fix / connection lifecycle), 20ac8bf (recovery), 00f5d00 (opcua_connect health check), f88e550 (docs)
- [x] Build and Test - `build-and-test-summary-readonly-session-recovery.md`: 312 unit tests, 11/11 integration tests

## Code Location Rules
- **Application Code**: Workspace root (NEVER in aidlc-docs/)
- **Documentation**: aidlc-docs/ only
- **Structure patterns**: existing `internal/{config,logger,mcp,opcua}` layout; new code follows
  the same pattern (e.g. a new `internal/store` package for Phase 2)

## Reverse Engineering Status
- [x] Reverse Engineering - Completed on 2026-08-02T00:00:00Z
- **Artifacts Location**: aidlc-docs/inception/reverse-engineering/

## Extension Configuration
| Extension | Enabled | Decided At |
|---|---|---|
| Resiliency Baseline | Yes (most cloud-deployment sub-rules N/A - experimental/lab tool, no hosted deployment target; RESILIENCY-10 dependency-isolation/graceful-degradation is the applicable one) | Requirements Analysis |
| Security Baseline | Yes | Requirements Analysis |
| Property-Based Testing | Yes (full enforcement, not Partial) | Requirements Analysis |

## Stage Progress
- [x] Workspace Detection - brownfield confirmed, no prior AIDLC artifacts, proceeding to Reverse Engineering
- [x] Reverse Engineering - 8 artifacts generated, approved by user 2026-08-02
- [x] Requirements Analysis - requirements.md generated, approved by user 2026-08-02
- [x] User Stories - assessed and skipped (single user type, well-defined tool contracts, no divergent user journeys); user accepted this recommendation
- [x] Workflow Planning - execution-plan.md generated, awaiting user approval
- [x] Application Design - 5 artifacts generated, awaiting user approval
- [x] Units Generation - 3 units defined, approved by user 2026-08-02
- [x] Build and Test - build-instructions.md, unit-test-instructions.md, integration-test-instructions.md, build-and-test-summary.md generated. All green: go build/vet/gofmt clean, go test -race ./... (194 tests, 0 failures), make test-integration (2/2 pass against real Docker). Self-approved (autonomous mode) 2026-08-02.

## Construction Progress
### Unit 1 - Persistent Store
- [x] Functional Design - domain-entities.md, business-rules.md, business-logic-model.md generated, approved by user 2026-08-02
- [x] NFR Requirements - nfr-requirements.md, tech-stack-decisions.md generated, approved by user 2026-08-02
- [x] NFR Design - nfr-design-patterns.md, logical-components.md generated, approved by user 2026-08-02
- [x] Infrastructure Design - SKIP (no infra-as-code in this project)
- [x] Code Generation - internal/store package created (types.go, value_encoding.go, store.go + tests), StoreConfig added, bbolt promoted to direct (v1.5.0), rapid added (v1.3.0). 154 tests passing, 85.5% coverage on internal/store. Approved by user 2026-08-02.

**Unit 1 COMPLETE.**

### Unit 2 - Subscription Management
- [x] Functional Design - domain-entities.md, business-rules.md, business-logic-model.md generated, approved by user 2026-08-02
- [x] NFR Requirements - nfr-requirements.md, tech-stack-decisions.md generated, approved by user 2026-08-02
- [x] NFR Design - nfr-design-patterns.md, logical-components.md generated, approved by user 2026-08-02
- [x] Infrastructure Design - SKIP (no infra-as-code in this project)
- [x] Code Generation - reconnect_watcher.go, subscription.go (SubscriptionManager, seam interfaces, notification pump), client.go extended (Subscribe, state-change channel), mock_subscription_test.go, reconnect_watcher_test.go, subscription_test.go, subscription_pbt_test.go (stateful PBT). 93 tests passing in internal/opcua. Approved by user 2026-08-02.

**Unit 2 COMPLETE.**

### Unit 3 - Read-Through Caching & MCP Integration
**Mode**: Autonomous - user stepped away, explicitly authorized self-resolving all
Functional/NFR Design questions using precedent from `requirements.md`/
`components.md`/`component-methods.md`/`services.md` and Unit 1/2 conventions,
without pausing for [Answer]-tag cycles or per-stage approval gates. Decisions
are documented inline in each design doc instead. See audit.md entry
"Unit 2 Approved - Autonomous Mode Enabled for Unit 3".
- [x] Functional Design - domain-entities.md, business-rules.md (BR-1..BR-11), business-logic-model.md generated, self-approved (autonomous mode) 2026-08-02
- [x] NFR Requirements - nfr-requirements.md, tech-stack-decisions.md generated, self-approved (autonomous mode) 2026-08-02
- [x] NFR Design - nfr-design-patterns.md (resolves SECURITY-01/13), logical-components.md generated, self-approved (autonomous mode) 2026-08-02
- [x] Infrastructure Design - SKIP (no infra-as-code in this project)
- [x] Code Generation - CachingClient (caching_client.go + unit/PBT tests), mcp.Server integration (3 new tools, opcua_read/write/browse_nodes wired to CachingClient, shutdown ordering), cmd/opcua-mcp.go wiring (store-open degradation per BR-11), testcontainers-go integration suite (both tests run and pass against a real Docker container), Makefile test-integration target, README updates. Self-approved (autonomous mode) 2026-08-02.

**Unit 3 COMPLETE. All 3 units done.**

## Bugfix Pass Progress (Custom-Node Read/Write Correctness)
- [x] Workspace Detection - brownfield, prior AIDLC artifacts present, proceeding directly to Requirements Analysis (Reverse Engineering already done)
- [x] Requirements Analysis - `requirements-custom-node-rw-fix.md` generated from interactive AskUserQuestion answers gathered during plan mode; self-approved (autonomous mode)
- [x] User Stories - SKIP (bug fix with clear, isolated-to-value-conversion scope, no new user-facing feature or divergent user journey)
- [ ] Workflow Planning - execution plan is the approved plan file itself (`i-want-you-to-lucky-candy.md`); no separate execution-plan.md needed given the plan's existing step-by-step ordering
- [x] Application Design - SKIP (no new components/services; `NormalizeReadValue` is a new function within the existing `internal/opcua` package boundary)
- [x] Units Generation - SKIP (single cohesive unit, see rationale above)
- [x] Construction (Functional Design / Code Generation, single unit) - complete, self-approved (autonomous mode). See audit.md "Construction Complete" entry for the full list of fixes.
- [x] Build and Test - `go build/vet/test -race ./...` (261 tests, 0 failures, up from 194 baseline), `go build/vet -tags=integration ./...`, `gofmt -l .`/`golint ./...` clean, `go test -tags=integration ./internal/opcua/...` (6/6 new integration tests pass against a real Docker container). Self-approved (autonomous mode) 2026-08-05.

**Bugfix pass COMPLETE.**

## Units (see unit-of-work.md for full detail)
1. **Persistent Store** (`internal/store`) - no new dependencies, foundational
2. **Subscription Management** (`SubscriptionManager` + `ReconnectWatcher`) - depends on Unit 1
3. **Read-Through Caching & MCP Integration** (`CachingClient`, `mcp.Server`, `cmd/opcua-mcp.go`, Docker/README, integration tests) - depends on Units 1 and 2

Build order: strictly sequential, Unit 1 → Unit 2 → Unit 3 (per user's Units Generation answers).
