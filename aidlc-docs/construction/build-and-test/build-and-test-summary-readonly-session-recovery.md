# Build and Test Summary - Read-Only Mode + Stale-Session Recovery

Run: 2026-10-28, autonomous mode.

## Commands and results

| Command | Result |
|---|---|
| `go build ./...` | clean |
| `go vet ./...`, `go vet -tags=integration ./...` | clean |
| `gofmt -l .`, `golint ./...` | no output |
| `go test -race ./...` | 312 tests, 0 failures (7 packages) |
| `go test -race -run '<PBTs>' -rapid.checks=2000` | all properties hold (session recovery x3, tool policy x2) |
| `go test -tags=integration ./internal/opcua/...` | 11/11 pass against `mcr.microsoft.com/iot/opc-ua-test-server:2.8` |

## Coverage of requirements

| Requirement | Tests |
|---|---|
| RO-2..RO-6, RO-8 | `TestToolsListDiscovery`, `TestDirectCallDenied`, `TestDirectCallAllowed` (through mcp-go `HandleMessage`) |
| RO-4 | `TestPresetCoversRegisteredTools`, `TestToolPolicyMatchesSetModel` (PBT) |
| RO-7 | `TestUnknownAllowedToolFailsSetup` |
| RO-1 | `TestReadOnlyEnvVars`, `TestDefaultValues` |
| SR-1..SR-4 | `TestReadRecoversFromStaleSession`, `TestReadRetriesOnlyOnce`, `TestNonStaleErrorDoesNotReconnect`, `TestSessionRecoveryRetriesAtMostOnce` (PBT), `TestRecoverIsIdempotentPerGeneration` (PBT), `TestIsStaleSessionErrorWrapDepth` (PBT) |
| SR-5 | `TestWriteStaleReconnectsWithoutRetry` |
| SR-6 | `TestBatchReadStaleSkipsIsolation` |
| SR-7 | `TestReconnectHandlerRunsAfterRecovery`, `TestSubscriptionManagerResubscribesOnRecovery` |
| SR-8 | `TestDisconnectIsNotRecovered`, `TestStateForwarderDropsRetiredGeneration` |
| SR-9 | `TestEnsureConnectedOutcomes`, `TestIntegrationEnsureConnectedRebuildsInvalidSession` |
| Real server invalidation | `TestIntegrationRecoversFromServerSideSessionClose` (server returns BadSessionIDInvalid), `TestIntegrationRecoversAfterServerRestart` (container restart behind a test TCP proxy) |

## Defect found during testing

`TestRecoverIsIdempotentPerGeneration` failed on the first implementation: during a reconnect the old connection was already marked disconnected, so concurrent callers failed with "client is not connected" instead of waiting. Fixed by keeping the old connection published until the new one is dialed and tracking the generation alongside the client (`clientGen`).

## Extension compliance

| Rule | Status |
|---|---|
| SECURITY-08 function-level authorization | Compliant: denial enforced server-side in middleware, not only by hiding |
| SECURITY-15 fail closed | Compliant: unclassified tools denied in read-only mode, unknown allowlist entry fails startup, failed reconnect leaves the client disconnected |
| SECURITY-03 logging | Compliant: reconnect logs carry the operation name only, no values or node IDs |
| Other SECURITY rules | N/A (no new network surface, storage, or credentials) |
| RESILIENCY-10 timeouts and degradation | Compliant: probe and close bounded by request timeout, recovery failure returns the original error with context |
| Other RESILIENCY rules | N/A (lab tool, no deployment target, per prior runs' configuration) |
| PBT-01..04, PBT-05 oracle, PBT-09 framework (rapid) | Compliant: properties listed in functional design, all implemented |
