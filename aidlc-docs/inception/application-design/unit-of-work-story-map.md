# Unit-to-Requirement Map — Phase 2

User Stories was skipped for this pass, so this maps `requirements.md`'s
functional/non-functional requirements to units instead of stories to
units.

| Requirement | Description | Unit |
|---|---|---|
| FR-1.1–1.5 | `internal/store` package, bbolt wrapper, buckets, typed CRUD, lifecycle | Unit 1 |
| FR-5.1 | `StoreConfig` | Unit 1 |
| FR-2.1–2.6 | `SubscriptionManager`, pump, persistence, warm-start, reconnect handling | Unit 2 |
| FR-3.1–3.7 | Read-through caching (`opcua_read`, browse, typeinfo), write invalidation, `EnableCache` toggle | Unit 3 |
| FR-4.1–4.4 | 3 new MCP tools | Unit 3 |
| FR-5.2 | `SearchConfig` cleanup (`EnableCache` repurposed, dead fields removed) | Unit 3 |
| NFR-1 (reliability) | Timeouts/graceful degradation, shutdown ordering | Units 1 (store timeout), 2 (subscription shutdown), 3 (overall shutdown ordering) |
| NFR-2 (security) | Logging hygiene, dependency pinning, error handling | All units |
| NFR-3 (PBT) | Store round-trip (Unit 1), stateful subscription PBT (Unit 2) | Units 1, 2 |
| NFR-4 (integration testing) | testcontainers + Microsoft OPC-UA test server | Unit 3 (only point where the full stack is testable end-to-end) |
| NFR-5 (deployment) | Docker/README updates for the new DB path | Unit 3 |

Every FR/NFR from `requirements.md` is assigned to exactly one primary unit
(cross-cutting NFRs like security/reliability apply to all three, as noted).
No orphaned requirements.
