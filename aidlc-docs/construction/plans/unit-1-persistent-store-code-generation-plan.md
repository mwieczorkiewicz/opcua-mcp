# Code Generation Plan - Unit 1: Persistent Store

**Workspace root**: `/Users/mikolajwieczorkiewicz/software-engineering/opcua-mcp`
(from `aidlc-docs/aidlc-state.md`). Brownfield - `internal/config/config.go`
already exists (modify in-place); `internal/store/` doesn't exist yet
(create new).

## Unit Context
- **Requirements covered**: FR-1.1–1.5, FR-5.1 (`requirements.md`)
- **Dependencies**: none on other units (Unit 1 is foundational)
- **Interfaces produced**: `store.Store` and its per-bucket CRUD methods -
  the contract Units 2 and 3 will depend on via their own narrow
  consumer-defined interfaces (not created in this unit)
- **Entities owned**: `ValueEntry`, `TypeInfoEntry`, `BrowseEntry`,
  `BrowseReference`, `SubscriptionIntent`, plus the internal
  `encodedValue`/`valueKind` type-fidelity mechanism

## Steps

- [x] **Step 1 - Dependency updates**: `go get go.etcd.io/bbolt@v1.5.0` (promote
      to direct, per `tech-stack-decisions.md`); `go get pgregory.net/rapid@v1.3.0`
      (new test-only direct dependency); `go mod tidy`. Verify the bbolt
      v1.4.0→v1.5.0 diff doesn't change any of the API surface verified
      earlier (`Open`/`Options.Timeout`/`Update`/`View`/
      `CreateBucketIfNotExists`/`Get`/`Put`/`Delete`/`ForEach`/`Close`)
      before proceeding - same discipline as the gopcua upgrade.

- [x] **Step 2 - Config**: add `StoreConfig` struct to
      `internal/config/config.go` (`envPrefix "STORE_"`): `DBPath`
      (default `mcp_opcua_store.db`), `OpenTimeout` (default `5s`),
      `TypeInfoTTL` (default `24h`), `BrowseTTL` (default `5m`),
      `BatchWindow` (default `25ms`), `BatchMaxItems` (default `250`),
      `NotifyChanBuffer` (default `1024`). Add `Store StoreConfig` field to
      the top-level `Config` struct. (Only `DBPath`/`OpenTimeout` are used
      by Unit 1 itself; the rest are declared now since they're all part of
      one cohesive config struct per `requirements.md` FR-5.1, but consumed
      by Units 2/3.)

- [x] **Step 3 - Domain entities**: create `internal/store/types.go` per
      `domain-entities.md` - `ValueEntry`, `TypeInfoEntry`, `BrowseEntry`,
      `BrowseReference`, `SubscriptionIntent`, `valueKind` constants,
      `encodedValue` struct.

- [x] **Step 4 - Value encoding**: create `internal/store/value_encoding.go`
      - `encode(interface{}) (encodedValue, error)` / `decode(encodedValue)
      (interface{}, error)` per `business-logic-model.md`'s algorithm
      (closed type switch, fail-fast on unknown types, recursive for
      `[]interface{}`).

- [x] **Step 5 - Store core**: create `internal/store/store.go` - `Store`
      struct, bucket name constants, `Open(path, timeout)`/`Close()` (no
      `ctx` - NFR Requirements scoped the `ctx` addition to the per-bucket
      CRUD methods only, not `Open`/`Close`, which are one-time
      startup/shutdown calls) per `business-logic-model.md`'s `Open`
      flowchart (wrapped errors per BR-5, `CreateBucketIfNotExists` x4 per
      BR-6, `0600` file mode).

- [x] **Step 6 - Values bucket CRUD**: add `GetValue`/`PutValue`/
      `DeleteValue` to `store.go`, using `encode`/`decode` from Step 4,
      `ctx context.Context` first parameter (NFR Requirements Q4=B),
      non-empty-key validation (BR-3).

- [x] **Step 7 - TypeInfo/Browse/Subscriptions bucket CRUD**: add
      `GetTypeInfo`/`PutTypeInfo`, `GetBrowse`/`PutBrowse`,
      `GetSubscription`/`PutSubscription`/`DeleteSubscription`/
      `ListSubscriptions` to `store.go` (direct JSON marshal/unmarshal, no
      encoding needed per BR-2's scope).

- [x] **Step 8 - Unit tests**: create `internal/store/store_test.go` -
      table-driven tests against a real bbolt instance in `t.TempDir()`:
      `Open`/`Close` round-trip, all 4 buckets exist after open, CRUD
      round-trip per bucket, missing-key returns `(zero, false, nil)` not
      an error, empty-key returns an error (BR-3), a second `Open` against
      an already-locked file returns within `OpenTimeout` rather than
      hanging (mirrors the acceptance criterion the now-deleted original
      plan specified for this exact behavior).

- [x] **Step 9 - Property-based tests**: create
      `internal/store/value_encoding_test.go` - `rapid`-driven round-trip
      property test for `encode`/`decode` across the full closed type set
      (bool, all int/uint widths, float32/64, string, time.Time, []byte,
      nested `[]interface{}` arrays), asserting `decode(encode(v)) == v`
      for generated inputs of each type; a table-driven example-based test
      for the fail-fast case (unsupported type returns an error) - PBT
      complements, doesn't replace, example-based coverage (PBT-10).

- [x] **Step 10 - Documentation summary**: create
      `aidlc-docs/construction/unit-1-persistent-store/code/summary.md`
      (markdown only, per Code Location Rules) summarizing what was built,
      linking back to the functional/NFR design docs.

## Story/Requirement Traceability
Every step maps to `requirements.md` FR-1 (Steps 1, 3-9) or FR-5.1 (Step 2)
- see `unit-of-work-story-map.md` for the full FR-to-unit mapping. No step
is unaccounted for.

## Verification (deferred to Build and Test, but noted here per this unit's scope)
`go build ./... && go vet ./... && gofmt -l . && go test -race ./internal/store/...`
must be green before this unit is considered complete, matching the
discipline held throughout Phase 0/1 and this session's every subsequent
commit.
