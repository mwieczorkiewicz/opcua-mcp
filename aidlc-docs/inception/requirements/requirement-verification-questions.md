# Requirements Clarification Questions - Phase 2 (Subscriptions + Persistent Cache)

Please answer each question by filling in the letter choice after the `[Answer]:` tag. If none of the options match, choose the last option (Other) and describe your preference. Let me know when you're done.

## Question 1 - Scope: subscriptions and persistent cache together, or split?

The plan bundles two things: (a) push-based subscriptions, and (b) a
persistent bbolt-backed cache (used for both subscribed values and
read-through caching of ordinary reads/browses). They're related - the
subscription pump writes into the same cache - but not strictly coupled;
the cache could land without subscriptions.

A) Build both together in this pass, as originally scoped

B) Build the persistent cache first (read-through caching for `opcua_read`/`opcua_browse_nodes`, plus the `typeinfo` cache), defer subscriptions to a later pass

C) Build subscriptions first (in-memory only, no persistence), defer the bbolt-backed cache to a later pass

D) Other (please describe after [Answer]: tag below)

[Answer]:A)

## Question 2 - Subscription persistence across process restart

`docs/plan/plan.md` (now removed) envisioned a `subscriptions` bucket
storing intent (node IDs + interval) that survives a full process restart -
so subscriptions set up before a restart automatically resume afterward,
without the client needing to call `opcua_subscribe` again.

A) Yes - persist subscription intent and auto-resume on startup (more complex: needs the "warm-load" + re-subscribe-on-startup logic)

B) No - subscriptions only need to survive a live reconnect (gopcua's own AutoReconnect handles that internally, confirmed in v0.9.0); a process restart clears them, client must resubscribe

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 3 - Read-through cache write-invalidation

After a successful `opcua_write` to a node that's only read-through cached
(not subscribed), should the cache entry for that node be invalidated
immediately, or left to expire naturally per `max_age_ms`?

A) Invalidate immediately on successful write (closes the staleness window for a write-then-read-back pattern - a plausible common LLM usage pattern)

B) Leave it to expire naturally (simpler; document the staleness window in the tool description)

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 4 - Cache TTL / batching defaults

The removed plan proposed specific defaults: `STORE_TYPEINFO_TTL=24h`,
`STORE_BROWSE_TTL=5m`, `STORE_BATCH_WINDOW=25ms`, `STORE_BATCH_MAX_ITEMS=250`,
`STORE_OPEN_TIMEOUT=5s`, `STORE_DB_PATH=./opcua_store.db`.

A) Use these defaults as proposed

B) I want to adjust some of these - I'll specify which after the [Answer]: tag below

C) Other (please describe after [Answer]: tag below)

[Answer]: B) STORE_DB_PATH should be mcp_opcua_store.db, rest is fine.

## Question 5 - bbolt version

`go.etcd.io/bbolt` v1.4.0 is currently an indirect dependency (already verified compatible with everything the store package needs). It's about to be promoted to direct.

A) Promote v1.4.0 to direct as-is; upgrade separately later if ever needed

B) Bundle a bump to the latest v1.5.x while promoting to direct, since it's already being touched

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

## Question 6 - `EnableCache`/dead config field cleanup

`SearchConfig.EnableCache` (`SEARCH_ENABLE_CACHE`) currently gates nothing
(cosmetic only); `CacheTTL`/`MaxCacheSize` are parsed but unused entirely.
The plan is to repurpose `EnableCache` as the master on/off switch for the
new read-through behavior.

A) Repurpose `EnableCache` as the master toggle for read-through caching (when false, `opcua_read`/browse always go live); remove the still-unused `CacheTTL`/`MaxCacheSize` fields

B) Leave existing `Search*` config fields untouched; use new, separate `STORE_*` env vars exclusively for Phase 2's cache behavior

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 7 - Access to a real/simulated OPC-UA server for verification

Unit tests use a mock (`opcuaClient` interface seam) exclusively - they
can't verify gopcua's actual subscription/reconnect wire behavior
end-to-end. The README documents a `make start-opcua-server` target using a
Microsoft OPC UA test server in Docker.

A) Yes, I can run the local Docker test server (`make start-opcua-server`) to manually verify subscription behavior once the code lands

B) No live/simulated server available for this pass - ship with unit-test coverage only, document the manual-verification gap explicitly

C) Other (please describe after [Answer]: tag below)

[Answer]: C) Implement e2e/integration tests with Microsoft OPCUA Test Server. You can use test containers if reasonable.

## Question 8 - New MCP tool count / naming

Proposed: `opcua_subscribe`, `opcua_unsubscribe`, `opcua_list_subscriptions`
- 3 new tools, following the existing naming convention.

A) Proceed with these 3 tools as named

B) I want different names or a different tool split - I'll specify after the [Answer]: tag below

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 9 - `opcua_read` backward compatibility

Plan: adding `max_age_ms` (optional, default `0`) to `opcua_read` must
produce byte-identical output to today's shape (`node_id`/`value`/`status`/
`source_timestamp`/`server_timestamp`) when `max_age_ms` is omitted, with
`source`/`cached_at` as strictly additive new fields.

A) Yes - this backward-compatibility constraint is required

B) No - feel free to change the response shape if it simplifies the implementation (this would be a breaking change requiring a README changelog callout, same bar as prior breaking changes this session)

C) Other (please describe after [Answer]: tag below)

[Answer]: C) Feel free to change the response shape as needed and as reasonable. No one uses it yet anyway.

## Question 10 - Docker/deployment updates for the new persistent file

The bbolt DB file needs a writable, ideally persistent, path in
containerized deployments (similar to `search_index/` today).

A) Update `Dockerfile`/`README.md` Docker examples to document/mount the new DB path as part of this work

B) Defer Docker/deployment documentation to a later pass - just make the default path work correctly for non-containerized (local/dev) use for now

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 11 - Resiliency baseline extension

*Extension opt-in.* This project is a bridge to live industrial control
systems - subscriptions/reconnect handling is exactly the kind of thing the
resiliency baseline's practices (fault tolerance, observability,
recoverability) are aimed at.

Should the resiliency baseline be applied to this project?

**What this extension is.** Enabling it applies a set of **directional, design-time best practices** for building resilient systems, derived from the **AWS Well-Architected Framework (Reliability Pillar)** and resilience-review guidance. It steers requirements, design, and code toward fault tolerance, high availability, observability, and recoverability - covering 15 practice areas across business goals, change management, observability, high availability, disaster recovery, and continuous improvement.

**What this extension is NOT.** Enabling it does **not** make your workload production-ready, nor does it certify or guarantee any availability, RTO, or RPO target. It is a **starting point** that scaffolds good resiliency decisions early - it is not a substitute for a formal **AWS Well-Architected Review** of the built system.

A) Yes - apply the resiliency baseline as directional best practices and design-time guidance

B) No - skip the resiliency baseline

X) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 12 - Security baseline extension

*Extension opt-in.*

Should security extension rules be enforced for this project?

A) Yes - enforce all SECURITY rules as blocking constraints (recommended for production-grade applications)

B) No - skip all SECURITY rules

X) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 13 - Property-based testing extension

*Extension opt-in.*

Should property-based testing (PBT) rules be enforced for this project?

A) Yes - enforce all PBT rules as blocking constraints (recommended for projects with business logic, data transformations, serialization, or stateful components - this project has all four: type conversion/validation, JSON/bbolt serialization, and the subscription state machine)

B) Partial - enforce PBT rules only for pure functions and serialization round-trips

C) No - skip all PBT rules

X) Other (please describe after [Answer]: tag below)

[Answer]: A)
