# Functional Design - session-recovery

## Components (all in `internal/opcua`)

- `isStaleSessionError(err) bool` - true if the chain contains `ua.StatusBadSessionIDInvalid` or `ua.StatusBadServerNotConnected`.
- `Client.connGen atomic.Uint64` - generation of the live connection. Bumped whenever a connection is created or retired.
- `Client.connectMu sync.Mutex` - serializes every connect, reconnect and disconnect.
- `Client.onReconnect func(context.Context)` - set by `SubscriptionManager` via `SetReconnectHandler`; called after a recovery reconnect replaced a live connection.
- `withSessionRecovery[T]` - runs a read-only op, on a stale session reconnects (if still on the same generation) and retries once.
- `Client.recover(ctx, gen)` - serialized replace of the live connection.
- `Client.EnsureConnected(ctx) (ConnectOutcome, error)` - used by `opcua_connect`.

## Business Rules

| ID | Rule |
|---|---|
| BR-1 | State changes are delivered to the registered state channel through gopcua's `StateChangedFunc`, tagged with the generation of the connection that produced them. A state from a retired generation is dropped. This stops the `ReconnectWatcher` from treating an intentional `Close()` (disconnect or recovery) as a permanent death. |
| BR-2 | Retiring a connection = bump `connGen`, mark not connected (under `mu`), then `Close()` it outside `mu` with a bounded context. |
| BR-3 | `recover(ctx, gen)`: under `connectMu`; if `connGen != gen` another caller already reconnected, return nil; if not connected (user disconnected), return "not connected" (never resurrect); otherwise retire and `connectLocked`, then call `onReconnect` after releasing `connectMu`. |
| BR-4 | `withSessionRecovery`: capture `gen`, run op; on a stale error call `recover(ctx, gen)`; on success run op exactly once more and return its result; on recovery failure return the original error wrapped with the recovery error. |
| BR-5 | Leaf read ops are wrapped; composites (`GetNodeTypeInfo`, `GetServerInfo`) are not, so a single failure is retried once, not once per nesting level. |
| BR-6 | `Write` re-snapshots the client after fetching type info, so a reconnect during type-info fetch is used by the write. A stale write triggers `recover` and returns an error saying the write was rejected and not applied; no retry. |
| BR-7 | `Read` batch failure with a stale error returns the error (recovery handles it) instead of the per-node isolation fallback. |
| BR-8 | `EnsureConnected`: not connected => connect (outcome `Connected`). Connected and gopcua state is `Connected` and the probe read of i=2259 succeeds => `AlreadyHealthy`. Otherwise rebuild via `recover` (outcome `Reconnected`). Probe bounded by `RequestTimeout`. |
| BR-9 | `SubscriptionManager` registers `resubscribe` (drop in-memory groups, warm-start from persisted intent) as the reconnect handler; `rebuild` (watcher path) becomes `Connect` + `resubscribe`. Handler runs with `context.WithoutCancel(ctx)` so a cancelled request does not abort resubscription. |

## Testable Properties (PBT-01)

| Property | Category | Test |
|---|---|---|
| P1: for any sequence of read ops and injected stale failures, every op calls the server at most twice, and an op whose first attempt fails stale and second succeeds returns success | Invariant | `TestSessionRecoveryRetriesAtMostOnce` (rapid) |
| P2: N concurrent callers failing on the same stale generation cause exactly one reconnect | Idempotence (recover is idempotent per generation) | `TestRecoverIsIdempotentPerGeneration` (rapid over N) |
| P3: non-stale errors never trigger a reconnect | Invariant | covered in P1 generator (mix of stale, other error, success) |
| P4: `isStaleSessionError` is true iff the chain contains one of the two statuses, at any wrap depth | Oracle | `TestIsStaleSessionErrorWrapDepth` (rapid) |

Example tests: write does not retry; batch read with stale error does not use the per-node fallback; disconnect is not resurrected; retired-generation state changes are dropped; `EnsureConnected` outcomes. Integration: real server rejects a session closed server-side mid-test, and a container restart mid-session.
