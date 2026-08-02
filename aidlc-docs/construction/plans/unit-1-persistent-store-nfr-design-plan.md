# NFR Design Plan — Unit 1: Persistent Store

## Plan
- [ ] Answer the question below
- [ ] Generate `nfr-design-patterns.md`
- [ ] Generate `logical-components.md`

## Category coverage (per the mandatory category checklist)
Most categories are already fully resolved by `nfr-requirements.md` and need
no new design question — restating why rather than skipping silently:

- **Scalability Patterns**: resolved (no sharding/custom indexing; bbolt's
  default B+tree is adequate at the confirmed thousands-of-entries scale).
- **Performance Patterns**: resolved (no benchmarking needed; local bbolt
  I/O isn't the bottleneck in this system).
- **Security Patterns**: resolved (no encryption at rest; file created
  `0600` per the existing `bbolt.Open(path, 0600, ...)` call already
  specified in `component-methods.md`).
- **Logical Components**: resolved — no queues/caches/circuit-breakers
  belong in this unit (it *is* the cache/persistence primitive other units
  use; adding another layer in front of it would be pointless).

One genuine open question remains:

## Question 1 — Resilience pattern: retry logic for bbolt operations?
Unlike network calls (which have transient failure modes retries can help
with), bbolt operations are local and either succeed or fail for a durable
reason (disk full, corruption, permissions) — a retry wouldn't change the
outcome.

A) No retry logic in `Store` — a bbolt operation failure is returned to the caller immediately, once. Retrying local I/O failures rarely helps and would just delay surfacing a real problem (matches this project's existing philosophy of fail-fast rather than silently degrading).

B) Add retry logic (e.g. N attempts with backoff) for bbolt operations — describe the specific failure mode you want retried after [Answer]: tag below.

[Answer]: A
