# Logical Components - Unit 1: Persistent Store

No additional logical components (queues, caches, circuit breakers, rate
limiters) are introduced in this unit - `Store` is itself the persistence
primitive; nothing sits in front of or behind it within Unit 1's scope.

| Logical Component | Present? | Notes |
|---|---|---|
| Queue | No | No async processing within `Store` itself - the pump/batching queue belongs to Unit 2's `SubscriptionManager` |
| Cache | N/A | `Store` *is* the cache's persistence backend, not a consumer of one |
| Circuit breaker | No | No external service calls from `Store` - bbolt is embedded/in-process |
| Rate limiter | No | No shared/contended resource requiring rate limiting at this scale |
| Retry wrapper | No (by design) | Per `nfr-design-patterns.md`'s fail-fast resilience pattern |

`Store`'s only structural component is the bbolt `*bbolt.DB` handle itself,
wrapped by the `Store` struct - already fully specified in
`component-methods.md`.
