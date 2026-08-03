# Telemetry

opcua-mcp collects anonymous, aggregate usage telemetry via
[Aptabase](https://aptabase.com) (EU-hosted), sent as one summary event per
flush interval (default 10 minutes, configurable via
`TELEMETRY_FLUSH_INTERVAL`) - never one event per MCP call. It's **on by
default**: this is a small open-source project maintained in limited spare
time, and knowing in aggregate which tools and features actually get used
(vs. which ones nobody touches) is what makes it possible to prioritize that
time well. If that tradeoff isn't for you, turning it off takes one
environment variable (see below) - and every code path this touches is
designed so a bug in it can never affect server correctness or availability:
telemetry sends are fire-and-forget, time out in 2 seconds, are never
retried, and a dropped event is simply dropped.

## What's collected

Always aggregated and bucketed - never per-event:

- Counts of MCP tool calls, by tool name (e.g. `opcua_read: 42`)
- Average number of parameters per call (a count, never the parameter values)
- Cache hit/miss counts
- Auth mode in use (`anonymous`/`username`/`certificate` - the mode name only)
- OPC-UA security policy in use (e.g. `Basic256Sha256` - the config enum value)
- Discovered node count, bucketed (`<100`, `100-1k`, `1k-10k`, `>10k`) - never
  the exact count
- Error counts by coarse category (e.g. `timeout`, `type_mismatch`) - never
  raw error message text
- Transport mode (stdio/http), server version, OS, architecture, session
  duration
- A random anonymous ID, persisted to `$XDG_CONFIG_HOME/opcua-mcp/telemetry_id`
  (not derived from or linkable to any hardware or network identifier)

## What's never collected, under any circumstance

- Node IDs, browse names, or node paths
- OPC-UA endpoint URLs, hostnames, or IP addresses
- Any value read from or written to a node
- Raw error message text
- Usernames, passwords, or certificate contents/paths
- Raw OPC-UA server `ApplicationName`/`ProductUri` strings

The exact `session_summary` JSON payload is documented at the top of
`internal/telemetry/telemetry.go`, and `internal/telemetry/telemetry_test.go`
(`TestSessionSummaryPayloadAllowlist`) enforces this list by construction -
the test fails if a field outside it is ever added, rather than relying on
code review alone to catch a leak.

## Opting out

| Variable | Effect |
|---|---|
| `DO_NOT_TRACK=1` | Disables telemetry - the cross-project community convention ([consoledonottrack.com](https://consoledonottrack.com)) |
| `OPCUA_MCP_TELEMETRY=false` | Disables telemetry - this project's own switch |

Either one is enough on its own; both accept `1`/`true`/`yes`/`on` and
`0`/`false`/`no`/`off` (case-insensitive), and `DO_NOT_TRACK` takes
precedence if both are set with conflicting values.
