# NFR Requirements Plan - Unit 1: Persistent Store

## Plan
- [ ] Answer the questions below
- [ ] Generate `nfr-requirements.md`
- [ ] Generate `tech-stack-decisions.md`

## Context
Most project-level NFRs were already settled in `requirements.md`'s
Extension Compliance tables (experimental/lab tool - no formal
availability/scalability/incident-response targets). This plan covers what's
specific to Unit 1 and still genuinely open.

## Questions

### Question 1 - PBT framework selection (required per PBT-09)
`requirements.md` NFR-3.4 flagged this as needing its own sign-off. The two
realistic Go options:

A) `pgregory.net/rapid` - lightweight, idiomatic Go, no test-framework lock-in (works with plain `testing.T`), smaller API surface to learn

B) `github.com/leanovate/gopter` - more mature/feature-rich (includes stateful-command-based testing helpers directly, relevant for Unit 2's `SubscriptionManager` PBT later), somewhat more ceremony to set up

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

### Question 2 - Encryption at rest for the bbolt file (SECURITY-01)
The bbolt file will contain cached OPC-UA process values, which could be
sensitive depending on what the connected industrial system controls.

A) No encryption at rest for this pass - the file inherits whatever protection the host filesystem/OS provides (matches this project's experimental/lab-deployment context; document as an accepted risk, revisit if this tool is ever used against a genuinely sensitive/regulated system)

B) Encrypt the bbolt file at rest - describe the preferred mechanism after [Answer]: tag below (e.g. OS-level disk encryption assumed, or application-level encryption of values before they reach bbolt)

[Answer]: A)

### Question 3 - Expected scale
Rough order of magnitude for cached nodes / active subscriptions, to sanity-check that bbolt's defaults (no custom indexing, no sharding) are appropriate.

A) Small - tens to low hundreds of nodes/subscriptions (typical for a single industrial cell/line in a lab setting)

B) Medium - thousands

C) Large - tens of thousands or more

D) Other (please describe after [Answer]: tag below)

[Answer]: B)

### Question 4 - Context/cancellation support in `Store`'s methods
bbolt's own `Update`/`View` don't take a `context.Context` - they're
in-process, local-disk operations, not network calls.

A) No `context.Context` parameters on `Store`'s methods - local bbolt I/O is fast enough (sub-millisecond typically) that cancellation support would add API surface without a real benefit; matches bbolt's own API shape

B) Add `ctx context.Context` to every method anyway, for consistency with `Client`'s methods (which do take `ctx`, since those cross the network) - even though it wouldn't do anything functionally today

C) Other (please describe after [Answer]: tag below)

[Answer]: B)
