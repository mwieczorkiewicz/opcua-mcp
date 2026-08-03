# NFR Requirements - Unit 1: Persistent Store

## Scalability
- **Expected scale**: Thousands of cached nodes/subscriptions (Q3=B). bbolt
  handles this comfortably with its default B+tree structure - no custom
  indexing, sharding, or pagination needed for `ListSubscriptions` at this
  scale (a full-bucket `ForEach` over a few thousand small JSON entries is a
  low-single-digit-millisecond operation on local disk).
- **Growth pattern**: Not a distinguishing design constraint at this scale -
  revisit only if actual usage approaches tens of thousands of entries per
  bucket (Q3's option C), at which point `ListSubscriptions`-style
  full-bucket scans might need pagination.

## Performance
- **Latency**: Not separately benchmarked for this pass - bbolt's
  documented characteristics (memory-mapped B+tree, sub-millisecond typical
  read/write on local disk) are adequate for a request path that already
  involves network round trips to the OPC-UA server elsewhere in the
  system; the store is never the bottleneck at the Q3 scale.
- **Throughput**: Bounded by `STORE_BATCH_WINDOW`/`STORE_BATCH_MAX_ITEMS`
  (Unit 2's concern, not Unit 1's) - `Store` itself has no batching logic;
  it's the batching caller's job to decide how many `PutValue` calls happen
  per bbolt transaction.

## Availability
N/A for this pass - carried over from `requirements.md`'s Extension
Compliance table (experimental/lab tool, no formal uptime/RTO/RPO targets).
`store.Open`'s non-zero timeout (functional design BR-5) is the one
concrete availability-adjacent behavior: it fails fast on a stale lock
rather than hanging indefinitely, which is a correctness property, not a
formal SLA.

## Security
- **SECURITY-01 (encryption at rest)**: **N/A for this pass** (Q2=A) - the
  bbolt file inherits whatever protection the host filesystem/OS provides.
  Documented as an accepted risk given this project's experimental/lab
  deployment context; revisit if this tool is ever pointed at a genuinely
  sensitive/regulated industrial system.
- **SECURITY-10 (supply chain)**: the new direct dependency (`bbolt`,
  bumped to the latest v1.5.x per the earlier Requirements Analysis answer)
  and the PBT framework (`pgregory.net/rapid`, below) are both pinned via
  `go.sum`.
- **SECURITY-15 (error handling)**: already covered by functional design's
  BR-3 (validation) and BR-5 (wrapped `Open` errors) - no bbolt error is
  ever swallowed.

## Tech Stack Selection
- **PBT framework**: `pgregory.net/rapid` (Q1=A) - see
  `tech-stack-decisions.md` for the rationale and the new-dependency
  sign-off this represents.

## Reliability
- Already covered by functional design (BR-4: no additional locking beyond
  bbolt's transactions; BR-6: idempotent bucket creation; BR-7: single
  `Close()` call, enforced by shutdown ordering in Unit 3).

## Maintainability
- Table-driven unit tests (project convention) for CRUD behavior against a
  real bbolt instance in `t.TempDir()` (no mock needed - `application-design.md`
  Question 4's answer B only applies to Units 2/3's *consumption* of
  `Store` via an interface; `Store`'s own tests exercise the real thing).
- Property-based round-trip tests (`rapid`) specifically for the
  `encode`/`decode` type-fidelity mechanism (functional design BR-2) - this
  is exactly the kind of round-trip property PBT-02 calls for, and it's the
  one piece of genuinely non-trivial logic in this unit.

## Usability
N/A - no UI, no end-user-facing surface (this package is internal-only,
never called directly by an MCP tool handler).

## API consistency addendum (from Question 4, answer B)
`Store`'s per-bucket CRUD methods gain a `ctx context.Context` first
parameter, for consistency with `Client`'s methods (which take `ctx` since
those cross the network) - even though `Store`'s bbolt calls don't
propagate it into `bbolt.Update`/`View` (no functional effect today beyond
an early `ctx.Err()` check before starting the bbolt transaction). This is
a refinement to `application-design/component-methods.md`'s signatures,
noted here rather than reopening that already-approved stage for a
backward-compatible addition.
