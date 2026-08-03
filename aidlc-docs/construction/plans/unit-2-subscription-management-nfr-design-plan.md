# NFR Design Plan - Unit 2: Subscription Management

## Plan
- [ ] Answer the question below
- [ ] Generate `nfr-design-patterns.md`
- [ ] Generate `logical-components.md`

## Category coverage
- **Resilience Patterns**: resolved - the reconnect rebuild logic itself
  (Functional Design BR-5, BR-8) *is* the resilience pattern for this unit;
  no circuit breaker needed on top (`Client.Connect` already has its own
  retry/backoff, and NFR Requirements Q1 explicitly rejected stacking a
  second one).
- **Scalability Patterns**: resolved - no new patterns beyond what Unit 1
  already established; `intervalGroups` stays small (bounded by distinct
  interval count, not node count).
- **Performance Patterns**: resolved - batching pattern already fully
  specified in Functional Design (BR-2's interval sharing + the pump's
  `BatchWindow`/`BatchMaxItems`).
- **Security Patterns**: resolved - nothing new beyond Unit 1's.

One open question:

## Question 1 - Server-side subscription/monitored-item limits
Real OPC-UA servers often cap max subscriptions per session and max
monitored items per subscription. Should `SubscriptionManager` enforce its
own configurable limit proactively, or rely entirely on the server
rejecting excess requests (which Functional Design's BR-4 already handles
gracefully as per-node partial failures)?

A) Rely entirely on server-side rejection via BR-4's existing partial-failure handling - no additional client-side limit. Simpler, and a hardcoded/configured client-side cap would just be guessing at a number the actual connected server may not even share (limits vary per OPC-UA server implementation).

B) Add a configurable client-side cap (e.g. `STORE_MAX_SUBSCRIPTIONS`) that rejects new `Subscribe` calls before even attempting them once exceeded - describe the desired default after [Answer]: tag below.

[Answer]: A)
