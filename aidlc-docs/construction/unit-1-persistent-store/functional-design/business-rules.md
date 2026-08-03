# Business Rules - Unit 1: Persistent Store

Answers referenced below are from `unit-1-persistent-store-functional-design-plan.md`.

## BR-1: No expiry/TTL logic inside `Store` (Q1=A)
`Get*` methods return whatever was last written, verbatim, with its
original timestamps - never age-filtered. Freshness comparisons
(`CachedAt`/`ReceivedAt` vs. a TTL) are exclusively the caller's
responsibility (`CachingClient` in Unit 3). `Store` has no notion of "TTL"
as a concept anywhere in its own code or config.

## BR-2: `ValueEntry.Value` type fidelity (Q2=B)
- Encoding: `PutValue` converts the incoming `interface{}` to an
  `encodedValue` via a type switch over exactly the closed set in
  `domain-entities.md` (`bool`, `int8`...`int64`, `uint8`...`uint64`,
  `float32`/`float64`, `string`, `time.Time`, `[]byte`, `[]interface{}`).
- **Fail-fast on unknown types**: a value whose dynamic type isn't in this
  set returns an error from `PutValue` - never silently falls back to
  default JSON encoding (which would defeat the entire point of this rule)
  and never panics.
- Decoding: `GetValue` reverses the process, reconstructing the exact
  original Go type from `encodedValue.Kind` + `Raw`/`Elems`.
- `TypeInfoEntry`/`BrowseEntry`/`SubscriptionIntent` don't need this
  treatment - none of their fields are `interface{}`; every field already
  has a concrete, JSON-round-trip-safe type (`uint32`, `int32`, `[]uint32`,
  `string`, `time.Time`, etc.).

## BR-3: Input validation (Q3=B)
- Every method taking a key (`nodeID`, `parentNodeID`, subscription `id`)
  returns an error immediately (before touching bbolt) if the key is empty.
- No other input validation - callers are trusted to pass well-formed
  entries (e.g. a `ValueEntry` with a zero-value `time.Time` is accepted;
  `Store` doesn't second-guess caller-supplied business data beyond the key
  being non-empty).

## BR-4: No additional locking beyond bbolt's own transactions (Q4=A)
`Store` methods use `db.View`/`db.Update` directly for every operation, no
`sync.Mutex`/`sync.RWMutex` field on `Store` itself. bbolt's own single-writer/
MVCC-reader model is the sole concurrency guarantee. (Note for Unit 2/3:
if a future operation needs multiple bucket reads/writes to appear atomic
as a unit, that's a single `db.Update` closure touching multiple buckets -
still no additional `Store`-level lock needed.)

## BR-5: `Open()` error handling (Q5=A)
- `Open` wraps any `bbolt.Open` failure: `fmt.Errorf("open store at %s: %w (a stale lock from a prior ungraceful shutdown is a common cause; check for another process holding the file)", path, err)`.
- This wrapping applies to any `bbolt.Open` error, not just a timeout -
  simpler than trying to distinguish timeout-vs-other-failure, and the hint
  is still directionally useful for most real failure modes (permissions,
  disk full, corrupt file) even when it's not literally a stale lock.

## BR-6: Bucket creation is idempotent and automatic
`Open` calls `CreateBucketIfNotExists` for all 4 buckets
(`values`/`typeinfo`/`browse`/`subscriptions`) unconditionally, every time -
whether the file is brand new or pre-existing. No separate "migration" or
"schema version" concept for this pass (out of scope; flagged as an
accepted future risk if fields are ever added to these structs in a
backward-incompatible way).

## BR-7: `Close()` is called exactly once, and only by the caller
`Store.Close()` simply calls the underlying `bbolt.DB.Close()` - no
internal "already closed" guard/idempotency, since `component-dependency.md`'s
shutdown ordering already guarantees exactly one call, strictly last. Adding
a redundant guard here would be defending against a caller bug that
`cmd/opcua-mcp.go`'s shutdown sequence (Unit 3) is responsible for not having.
