# Tech Stack Decisions - Unit 2: Subscription Management

## `github.com/google/uuid` v1.6.0 (existing indirect dependency, promoted to direct use)
- **Decision**: use `uuid.NewString()` for subscription IDs (Functional
  Design Q1).
- **Rationale**: already present in `go.mod` as an indirect dependency
  (pulled in via `mark3labs/mcp-go`/`gopcua`) at v1.6.0 - promoting an
  already-vetted, already-in-the-dependency-tree package to direct *use*
  carries none of the supply-chain risk a genuinely new dependency would,
  matching the precedent set for `bbolt` in Unit 1. No version change
  needed; v1.6.0 is what's already resolved.
- **License**: BSD-3-Clause (well-known, permissive; Google's standard
  license for this package).

## No other new dependencies
Unit 2 needs nothing beyond `gopcua/opcua` (already a direct dependency),
`google/uuid` (above), `internal/store` (Unit 1), `internal/config`, and Go
standard library (`sync`, `context`, `time`). `rapid` (Unit 1) is reused
for this unit's stateful PBT - no new test dependency either.
