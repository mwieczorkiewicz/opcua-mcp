# NFR Requirements - Unit 2: Subscription Management

## Reliability
- **Reconnect rebuild retry** (Q1=A): `ReconnectWatcher`'s `onPermanentDeath`
  calls `Client.Connect(ctx)` exactly once per detected permanent death. If
  that call exhausts `Client`'s own internal retry/backoff
  (`OPCUA_MAX_RETRIES`/`OPCUA_RETRY_DELAY`) and still fails, the failure is
  logged and the system waits for an externally-triggered reconnect (a
  manual `opcua_connect` tool call, or a process restart) rather than
  layering a second, independent retry/backoff policy on top. Two
  independently-configured retry loops stacked on each other risk an
  unbounded or confusing combined retry duration with no clear single
  place to reason about "how long will this actually keep retrying."
- Functional design's BR-6 (pump never crashes on a store failure) and BR-5
  (synchronous, single-attempt rebuild) are the other reliability-relevant
  decisions already locked in.

## Scalability
Carried over from Unit 1 (Q3=B, thousands of nodes/subscriptions) -
`intervalGroups` has at most one entry per distinct `intervalMs` value
actually in use, which will be far smaller than the node/subscription
count itself at this scale.

## Performance
No specific throughput target for the notification pump (Q3=A) - the
existing `STORE_BATCH_WINDOW`/`STORE_BATCH_MAX_ITEMS` defaults (25ms/250,
set in Unit 1) are the starting point; revisit only if real usage surfaces
a concrete bottleneck.

## Availability
N/A, carried over from the project-level resiliency-extension answers
(experimental/lab tool, no formal uptime targets).

## Security
No new secrets/credentials - subscriptions reuse the OPC-UA connection's
existing authentication. `SECURITY-15` (error handling on external calls)
is satisfied by BR-6 (pump) and BR-1 through BR-4's explicit error paths in
`Subscribe`/`Unsubscribe`.

## Tech Stack Selection
No new dependencies. `google/uuid` (Functional Design Q1) is an existing
indirect dependency promoted to direct use, same treatment as `bbolt` in
Unit 1 - not a new-dependency decision requiring separate sign-off.

## Testability
- **Stateful PBT** (Q2=A, resolves requirements.md NFR-3.3): a simplified
  in-memory reference model tracks "which node IDs are subscribed at which
  interval." `rapid`'s command-based stateful testing drives random
  `Subscribe`/`Unsubscribe` sequences against the real `SubscriptionManager`
  (backed by an extended `opcuaClient` mock seam covering `Subscribe`/
  `Monitor`/`Unmonitor`/`Cancel`), asserting after every command that:
  - `ListSubscriptions()` matches the model's view exactly.
  - Every `intervalGroup`'s `RefCount` equals the number of logical
    subscriptions in the model currently referencing that interval.
- This directly satisfies PBT-06's shape (simplified model + random command
  sequences + invariant checks after each command, not just at the end).
- Example-based tests still cover the specific scenarios PBT's shrinking
  might miss the intent of (e.g. the exact partial-failure response shape
  from BR-4, the exact reconnect-rebuild sequence from BR-5) - PBT
  complements, doesn't replace, per PBT-10.

## Maintainability
Same conventions as Unit 1: table-driven example tests, `internal/logger`
for all logging, `fmt.Errorf("...: %w", err)` wrapping throughout.
