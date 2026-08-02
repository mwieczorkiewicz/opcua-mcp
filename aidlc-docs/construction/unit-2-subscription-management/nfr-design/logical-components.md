# Logical Components — Unit 2: Subscription Management

| Logical Component | Present? | Notes |
|---|---|---|
| Queue | Yes (implicit) | `notifyCh` (a Go channel) is the queue between gopcua's publish loop and the pump goroutine - not a separate named component, just a buffered channel (`STORE_NOTIFY_CHAN_BUFFER`, Unit 1 config) |
| Cache | No (consumer, not owner) | Writes into `internal/store`'s `values` bucket (Unit 1) via the `subscriptionStore` interface - doesn't own or duplicate that cache |
| Circuit breaker | No | `Client.Connect`'s own retry/backoff is the closest analogue; no additional breaker needed (NFR Requirements Q1) |
| Rate limiter | No | No client-side subscription cap (NFR Design Q1) - the server is the rate/capacity authority |
| Retry wrapper | No (single-attempt by design) | BR-5/NFR Requirements Q1 - one rebuild attempt per detected death, relying on Client's own internal retries within that attempt |

No new infrastructure-level components beyond the channel already implicit
in gopcua's own `notifyCh` API and the pump goroutine consuming it.
