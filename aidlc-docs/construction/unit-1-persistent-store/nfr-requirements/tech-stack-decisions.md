# Tech Stack Decisions - Unit 1: Persistent Store

## `go.etcd.io/bbolt` v1.5.0 (promoted from indirect to direct)
- **Decision**: promote to direct at v1.5.0 - the latest stable release
  (confirmed via `go list -m -versions go.etcd.io/bbolt`: v1.5.0 is current,
  no beta/rc newer), per the Requirements Analysis answer to bundle a
  version bump while promoting to direct.
- **Rationale**: already an indirect dependency via `blevesearch/bleve`;
  its `Open`/`Options.Timeout`/`Update`/`View`/`CreateBucketIfNotExists`/
  `Get`/`Put`/`Delete`/`ForEach`/`Close` API was verified directly against
  v1.4.0 source during Phase 2 planning (all present with the exact
  signatures needed) - re-verify the diff between v1.4.0 and v1.5.0 during
  Code Generation before pinning, in case anything changed (the same
  discipline applied to the gopcua v0.8.0→v0.9.0 upgrade earlier this
  session).
- **License**: MIT (verified during Reverse Engineering).

## `pgregory.net/rapid` v1.3.0 (new direct, test-only dependency)
- **Decision**: `rapid` over `gopter` (Q1=A).
- **Rationale**:
  - Lightweight - works directly with `*testing.T`, no separate test
    runner or DSL to learn, consistent with this project's existing
    plain-`testing`-package convention (no testify, no ginkgo elsewhere in
    this codebase).
  - `gopter`'s main advantage (built-in stateful/command-based testing
    helpers) matters more for Unit 2's `SubscriptionManager` PBT
    (NFR-3.3) than for Unit 1's simple round-trip property (NFR-3.1) - but
    `rapid` also supports stateful testing via its own (simpler) state
    machine helpers, so choosing it now doesn't foreclose Unit 2's needs.
  - Smaller, more focused API surface reduces the learning/maintenance
    cost for a project that has no PBT experience yet.
- **Scope**: test-only dependency (`go.mod`'s `require` block for test
  deps, or a separate `tools`-style entry per Go module convention) - does
  not ship in the production binary.
- **License**: to verify during Code Generation (not yet checked - flagging
  here rather than assuming; expect a permissive license given typical Go
  ecosystem norms, but confirm before finalizing).

## No other new dependencies
`internal/store` needs nothing beyond bbolt (persistence),
`encoding/json`/`time`/`fmt`/`context` (Go standard library), and `rapid`
(tests only).
