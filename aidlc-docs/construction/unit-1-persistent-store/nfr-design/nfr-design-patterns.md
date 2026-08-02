# NFR Design Patterns — Unit 1: Persistent Store

## Resilience: fail-fast, no retry (Q1=A)
`Store` never retries a failed bbolt operation. Every method returns the
underlying error (wrapped per functional design BR-5 for `Open`,
unwrapped-but-propagated elsewhere) on the first failure. Rationale: bbolt
failures are local-I/O failures with durable causes (disk full, corruption,
permissions, a genuinely stale lock past the open timeout) — retrying
doesn't change the outcome, it only delays surfacing a real problem to the
caller. This is the same fail-fast philosophy functional design's BR-2
(unsupported value type) and BR-3 (empty key) already apply.

## Security: file permissions
`bbolt.Open(path, 0600, ...)` — owner-read-write-only, no group/world
access. Already specified in `component-methods.md`; restated here as the
concrete security pattern satisfying the "no encryption at rest, but don't
leave the file world-readable either" middle ground implied by NFR
Requirements' Q2=A answer (accepting no encryption doesn't mean accepting
no file permissions at all).

## Scalability/Performance: no patterns needed beyond bbolt's defaults
No caching-in-front-of-the-cache, no read replicas, no sharding — `Store`
already *is* the caching primitive; layering more infrastructure in front
of it would be solving a problem that doesn't exist at the confirmed
thousands-of-entries scale (NFR Requirements Q3=B).

## Testability pattern: real bbolt in `t.TempDir()`, not a mock
Reaffirming `application-design.md`'s decision: `Store`'s own tests use a
real, ephemeral bbolt instance rather than a mock, since bbolt is an
embedded/in-process library (no network, no external service to fake).
