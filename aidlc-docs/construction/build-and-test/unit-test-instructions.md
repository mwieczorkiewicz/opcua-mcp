# Unit Test Execution

## Run Unit Tests

### 1. Execute All Unit Tests
```bash
go test ./...
# race-detector variant (this project's standard local baseline):
go test -race ./...
# verbose + coverage:
go test -v -coverprofile=coverage.out ./...
# or: make test / make test-coverage
```

### 2. Review Test Results
- **Expected**: all 5 packages with test files pass, 0 failures (`cmd` has
  no test files - `main` is a thin composition root, exercised by the
  packages it wires together). 194 `=== RUN` entries (including
  table-driven subtests) across `internal/{config,logger,mcp,opcua,store}`
  as of this AIDLC run's completion.
- **Test Coverage** (`go test -cover ./...`): `internal/config` 96.3%,
  `internal/logger` 7.9% (mostly thin `slog` wrapper glue, not
  meaningfully more testable), `internal/mcp` 38.1% (tool handlers are
  mostly argument-parsing/formatting around already-tested
  `internal/opcua` logic - the value is in `internal/opcua`'s coverage),
  `internal/opcua` 50.9%, `internal/store` 85.5%. No enforced minimum
  threshold exists in this project; these are reported as a baseline, not
  a gate.
- **Test Report Location**: `coverage.out`/`coverage.html` (via
  `make test-coverage`), not committed to the repo.

### 3. Fix Failing Tests
If tests fail:
1. Run the specific failing package verbosely:
   `go test ./internal/<package>/... -run <TestName> -v`.
2. This project's test style is table-driven with mocked seams
   (`opcuaClient`/`subscribingClient`/`subscriptionHandle`/
   `valueTypeBrowseStore` in `internal/opcua`) rather than a live/simulated
   OPC-UA server - a failure almost always means either a real regression
   or a mock's canned response no longer matching the code path it stubs,
   not flaky infrastructure.
3. Property-based tests (`pgregory.net/rapid`, in
   `internal/store/value_encoding_test.go` and
   `internal/opcua/{subscription,caching_client}_pbt_test.go`) print the
   failing seed/shrunk case on failure - rerun with `-run` targeting just
   that test to iterate.

## Notes on test runtime
`internal/opcua`'s suite takes ~29-33s (dominated by two stateful/property
tests: `TestSubscriptionManagerStatefulProperties` and
`TestCachingClientReadTTLInvariant`, each constructing many fresh
bbolt-backed instances across ~100 `rapid.Check` trials). This was
evaluated during Unit 2's code generation and accepted as inherent
per-trial I/O setup cost, not a structural test-writing issue - it stays
well within any normal CI timeout.
