# Integration Test Instructions

## Purpose
Verify this project's assumptions about real OPC-UA/gopcua wire behavior -
subscription push delivery and read-through caching - against an actual
OPC-UA server, complementing the mocked unit tests in
`internal/opcua/{subscription,caching_client}_test.go` (which verify this
project's own logic, assuming gopcua behaves as documented).

## Test Scenarios

### Scenario 1: Subscribe → real push update → values bucket updated
- **Description**: `TestIntegrationSubscribePushesRealValueChangesIntoStore`
  (`internal/opcua/subscription_integration_test.go`).
- **Setup**: `testcontainers-go` starts
  `mcr.microsoft.com/iot/opc-ua-test-server:2.8` (the same tag pinned in
  `docker-compose.yml`); a real `*opcua.Client` connects to it.
- **Test Steps**: `SubscriptionManager.Subscribe` the standard
  `Server_ServerStatus_CurrentTime` node (`i=2258`, continuously changes)
  at a 500ms interval; poll the store's `values` bucket for up to 20s.
- **Expected Results**: the bucket entry for `i=2258` appears with
  `Source: "subscription"` and a non-nil value - confirming gopcua's real
  `Subscribe`/`Monitor`/notification-delivery wiring works end-to-end.
- **Cleanup**: `t.Cleanup` terminates the container and stops the
  `SubscriptionManager` automatically.

### Scenario 2: Read-through cache hit/miss/expiry against a real server
- **Description**: `TestIntegrationReadThroughCacheHitMissExpiry`.
- **Setup**: same container; a `CachingClient` wraps the connected client.
- **Test Steps**: `Read` the session-stable `Server_ServerStatus_StartTime`
  node (`i=2257`) three times: once with `max_age_ms=0` (must go live),
  once with `max_age_ms=60000` (must hit the cache populated by the first
  read, same value), once more with `max_age_ms=0` (must go live again,
  per BR-2's "0 never hits a live-sourced entry" rule).
- **Expected Results**: `Source` is `"live"`, `"cache"`, `"live"` in that
  order; the cached value matches the original live value exactly.
- **Cleanup**: automatic via `t.Cleanup`.

### Scenario not implemented: forced disconnect/reconnect
`requirements.md` NFR-4.1 originally listed a disconnect/reconnect
scenario as a candidate. `nfr-requirements.md` (Unit 3) flagged this as
conditional on being achievable without flakiness; it was **descoped**
during code generation (see
`aidlc-docs/construction/unit-3-read-through-caching-mcp-integration/code/summary.md`'s
Deviations section) - forcing deterministic container-restart timing was
judged too likely to produce a flaky CI test, and `ReconnectWatcher`'s
state-transition logic already has full example-based coverage in
`internal/opcua/reconnect_watcher_test.go` (Unit 2). Manual verification
(stop/restart the OPC-UA container under `docker-compose.yml` while a real
server is running and confirm subscriptions resume) remains a documented,
un-automated fallback if this needs re-verifying later.

## Setup Integration Test Environment

### 1. Start Required Services
No manual setup needed - `testcontainers-go` starts and tears down the
OPC-UA test server container automatically per test. Requires only a
running local Docker daemon (`docker info` must succeed).

### 2. Configure Service Endpoints
Not applicable - each test discovers its own container's mapped port at
runtime via `testcontainers-go`'s `Host()`/`MappedPort()`.

## Run Integration Tests

### 1. Execute Integration Test Suite
```bash
make test-integration
# equivalent to: go test -tags=integration ./...
```

### 2. Verify Service Interactions
- **Test Scenarios**: the 2 scenarios above.
- **Expected Results**: both `PASS`; total runtime ~9-10s including two
  container starts/stops (confirmed during this AIDLC run's Unit 3 code
  generation).
- **Logs Location**: stdout, via `-v`; `testcontainers-go` also logs each
  container's lifecycle (create/start/wait/terminate) automatically.

### 3. Cleanup
Automatic - each test's `t.Cleanup` terminates its container immediately
after the test finishes, whether it passed or failed. No manual
`docker rm`/`docker stop` needed.
