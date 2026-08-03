# Functional Design Plan - Unit 1: Persistent Store

## Plan
- [ ] Answer the questions below
- [ ] Generate `business-logic-model.md`
- [ ] Generate `business-rules.md`
- [ ] Generate `domain-entities.md`

## Questions

### Question 1 - Does `Store` enforce TTL/expiry itself?
`application-design.md`'s `CachingClient` is what decides "is this cached
entry still fresh" (comparing `CachedAt`/`ReceivedAt` against a TTL it
owns). Does `Store` itself do any expiry logic, or is it a dumb
key-value store that just returns whatever was last written, verbatim,
leaving all freshness decisions to the caller?

A) `Store` is a dumb key-value store - no expiry logic inside it at all. It stores/returns entries verbatim, including their timestamps; every caller (`CachingClient`, `SubscriptionManager`) decides freshness itself. Simpler, keeps `Store` free of business logic that varies per-bucket (browse TTL ≠ typeinfo TTL).

B) `Store` enforces expiry itself - `Get*` methods return `(zero, false, nil)` for an expired entry even if the row technically exists, given a TTL parameter.

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 2 - Type fidelity for `ValueEntry.Value` through JSON round-trip
JSON encoding is lossy for some Go types relevant here (e.g. `int32` vs
`int64` both decode to `float64` via `encoding/json`'s default
`interface{}` unmarshaling; `[]byte` needs explicit base64 handling `encoding/json`
already does automatically). This matters because `ValueEntry.Value` is
`interface{}` and needs to survive being written from an OPC-UA-decoded
value and read back for cache hits.

A) Accept `encoding/json`'s default behavior (numbers become `float64` on read-back) and document it as a known, accepted characteristic - a cache hit's value type may not be identical to the live-read value's type for exact integer types, though the numeric value itself is correct. This is acceptable because MCP tool responses are JSON anyway (the numeric distinction is invisible to the LLM client either way).

B) Preserve exact type fidelity - store a type tag alongside the value and reconstruct the original Go type on read. More correctness, more code.

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 3 - Input validation inside `Store`
Should `Store`'s methods validate their inputs (e.g. reject an empty node
ID key), or trust callers entirely (it's an internal package, not
externally facing)?

A) Trust callers - no validation inside `Store`. It's internal-only (never called directly by an MCP tool handler), and every caller already has a valid node ID by construction (either parsed via `ua.ParseNodeID` upstream, or a subscription ID this project generates itself).

B) Validate - reject empty keys with an error.

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 4 - Does `Store` need its own additional locking?
bbolt's own transaction model already serializes writes (single writer)
and gives readers a consistent MVCC snapshot.

A) No additional locking needed in `Store` - rely entirely on bbolt's own transaction semantics (`Update`/`View`). This matches how this codebase already treats bbolt-adjacent code (no extra mutex layered on top of what the underlying primitive already guarantees).

B) Add an additional `sync.RWMutex` in `Store` on top of bbolt's own transactions - describe the specific race this guards against after [Answer]: tag below.

[Answer]: A)

### Question 5 - Error handling on `Open()` failure/timeout
If `bbolt.Open` times out (a locked file, e.g. from a prior ungraceful
shutdown) or otherwise fails:

A) Propagate the error wrapped with `fmt.Errorf("...: %w", err)` and a hint that a stale lock from a prior ungraceful shutdown is a likely cause - matches this codebase's existing error-wrapping convention, gives an operator a concrete next step (check for a stale process/lock) rather than a bare bbolt error string.

B) Just propagate bbolt's raw error unwrapped.

C) Other (please describe after [Answer]: tag below)

[Answer]: A)
