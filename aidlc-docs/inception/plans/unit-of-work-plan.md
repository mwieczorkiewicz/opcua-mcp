# Unit of Work Plan — Phase 2

## Context
User Stories was skipped for this pass (single user type, well-defined tool
contracts). Story grouping/story-map questions below are adapted to
**functional-requirement grouping** instead — `requirements.md`'s FR-1
through FR-5 stand in for stories as the thing units get mapped to.

## Plan
- [ ] Answer the questions below
- [ ] Generate `aidlc-docs/inception/application-design/unit-of-work.md`
- [ ] Generate `aidlc-docs/inception/application-design/unit-of-work-dependency.md`
- [ ] Generate `aidlc-docs/inception/application-design/unit-of-work-story-map.md` (FR-to-unit map, since no stories exist)
- [ ] Validate unit boundaries and dependencies
- [ ] Ensure all FRs are assigned to a unit

## Proposed Decomposition (starting point, not final)

Based on `application-design.md`'s dependency graph:

- **Unit 1 — Persistent Store**: `internal/store` package (FR-1). No
  dependency on other new components — foundational.
- **Unit 2 — Subscription Management**: `SubscriptionManager` +
  `ReconnectWatcher` (FR-2). Depends on Unit 1.
- **Unit 3 — Read-Through Caching & MCP Integration**: `CachingClient`,
  `mcp.Server` changes (3 new tools + `opcua_read`/`opcua_browse_nodes`
  integration), config changes, Docker/README updates (FR-3, FR-4, FR-5).
  Depends on Unit 1 and Unit 2 (new tools delegate to
  `SubscriptionManager`; read-through needs the "is subscribed" signal).

## Questions

### Question 1 — Story/FR Grouping: does the proposed 3-unit split make sense?
A) Yes — proceed with Unit 1 (store) / Unit 2 (subscriptions) / Unit 3 (caching + MCP integration) as proposed

B) Split further — e.g. separate `ReconnectWatcher` from `SubscriptionManager` into its own unit, or separate read-through caching from the MCP tool/Docker work

C) Combine further — e.g. merge Unit 1 and Unit 2 since both are "backend" work with no user-visible surface until Unit 3

D) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 2 — Dependencies: sequential or can units overlap?
Units 2 and 3 both depend on Unit 1; Unit 3 depends on Unit 2.

A) Strictly sequential — complete and test each unit fully before starting the next (safest, matches this session's incremental-commit discipline throughout Phase 0/1)

B) Unit 1 first, then Units 2 and 3 designed/built with awareness of each other in parallel (faster, more risk of rework if the Unit 2 API shifts under Unit 3)

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 3 — Team alignment
A) N/A — solo/single-contributor work (this session), no team-ownership boundaries to define

B) Multiple people will work on different units — I'll describe ownership after [Answer]: tag below

[Answer]: A)

### Question 4 — Technical considerations: does any unit need different scalability/deployment treatment?
A) No — all 3 units ship together in the same binary/container, same deployment model as today

B) Yes — describe after [Answer]: tag below

[Answer]: A)

### Question 5 — Business domain boundaries
A) The proposed split (persistence / subscription-domain / MCP-integration-domain) matches this project's existing domain boundaries (`internal/store`, `internal/opcua`, `internal/mcp` already mirror this)

B) A different domain grouping makes more sense — describe after [Answer]: tag below

[Answer]: A)
