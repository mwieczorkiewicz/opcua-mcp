# NFR Requirements Plan — Unit 2: Subscription Management

## Plan
- [ ] Answer the questions below
- [ ] Generate `nfr-requirements.md`
- [ ] Generate `tech-stack-decisions.md`

## Already resolved, carried over (no new question needed)
- **Scalability**: thousands of nodes/subscriptions (Unit 1 NFR Requirements
  Q3) - `intervalGroups` will have at most as many entries as there are
  distinct `intervalMs` values in use, almost certainly far fewer than the
  node/subscription count itself.
- **Security**: no new secrets/credentials - subscriptions reuse the OPC-UA
  connection's existing auth. `SECURITY-15` (error handling) is addressed by
  Functional Design's BR-6 (pump never crashes on a store failure).
- **Tech stack**: no new dependencies beyond `google/uuid` (already
  resolved in Functional Design Q1 as promoting an existing indirect dep).

## Questions

### Question 1 — Reconnect rebuild retry strategy
`Client.Connect(ctx)` already has its own internal retry loop
(`OPCUA_MAX_RETRIES`/`OPCUA_RETRY_DELAY`). If `ReconnectWatcher`'s
`onPermanentDeath` callback calls `Client.Connect(ctx)` and that **entire**
call fails (exhausts its own internal retries), what should happen?

A) Log the failure and give up until the next externally-triggered reconnect attempt (e.g. a manual `opcua_connect` tool call, or process restart) - no additional outer retry loop in `ReconnectWatcher` itself. Avoids stacking two independent retry/backoff policies on top of each other (`Client`'s own + a new one), which risks confusing interaction (e.g. unbounded combined retry duration).

B) `ReconnectWatcher` adds its own outer retry loop (with backoff) around the whole rebuild attempt (`Connect` + re-`Subscribe` everything), independent of `Client`'s internal retries - describe the desired backoff after [Answer]: tag below.

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 2 — Stateful PBT scope (resolves requirements.md NFR-3.3)
`rapid` (chosen in Unit 1) supports stateful/command-based testing. What
should the stateful PBT model for `SubscriptionManager` actually check?

A) A simplified in-memory model tracking "which node IDs are subscribed at which interval," driven by random `Subscribe`/`Unsubscribe` command sequences against the real `SubscriptionManager` (backed by the mocked `opcuaClient` seam) - asserting `ListSubscriptions()` always matches the model's view, and that `intervalGroups`' `RefCount` invariant (`RefCount == number of logical subscriptions referencing that interval`) holds after every command. This is exactly PBT-06's "simplified reference model + command sequences + invariant checks after each command."

B) A narrower scope - describe after [Answer]: tag below

[Answer]:A)

### Question 3 — Performance target for the notification pump
`STORE_BATCH_WINDOW` (25ms) / `STORE_BATCH_MAX_ITEMS` (250) were already
set as config defaults in Unit 1. Does Unit 2 need a specific throughput
target to design/test against (e.g. "must keep up with N notifications/sec
without the channel filling up"), or is this not a distinguishing concern
at this project's scale?

A) Not a distinguishing concern - the existing batch window/size config is a reasonable starting point; no specific throughput target to design against for this pass (matches the "experimental/lab tool" posture already established)

B) Yes, there's a specific throughput target - describe after [Answer]: tag below

[Answer]: A)
