# Logical Components — Unit 3: Read-Through Caching & MCP Integration

| Logical Component | Present? | Notes |
|---|---|---|
| Queue | No | No new queue - `CachingClient` calls are synchronous request/response, unlike Unit 2's pump |
| Cache | Yes (consumer, not owner) | `CachingClient` reads/writes Unit 1's `values`/`typeinfo`/`browse` buckets via the narrow `valueTypeBrowseStore` interface - doesn't own or duplicate that storage |
| Circuit breaker | No | A cache/store failure degrades to "go live" per call (NFR Design's error-handling pattern above), not a breaker that trips across calls - each call independently decides fresh |
| Rate limiter | No | No new rate limiting - `Client.Read`'s existing behavior (and OPC-UA server-side limits) are unchanged |
| Retry wrapper | No | `CachingClient`'s live fallback is a single attempt, same as `Client.Read`/`Browse`/`Write` today - no new retry layer |

No new infrastructure-level components. Unit 3 is entirely decorator logic
(`CachingClient` wrapping `*Client`) plus thin MCP tool handlers - no queue,
breaker, limiter, or retry wrapper is warranted at this project's scale.
