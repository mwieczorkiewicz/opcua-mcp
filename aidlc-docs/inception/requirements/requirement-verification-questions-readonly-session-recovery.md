# Requirements Clarification Questions - Read-Only Mode + Stale-Session Recovery

Please answer each question by filling in the letter choice after the `[Answer]:` tag. If none of the options fit, choose the last option (Other) and describe your preference.

Context found during workspace detection:

- 18 tools are registered today: `opcua_read`, `opcua_write`, `opcua_browse`, `opcua_node_info`, `opcua_server_info`, `opcua_connect`, `opcua_disconnect`, `opcua_get_value`, `opcua_browse_nodes`, `opcua_get_value_by_name`, `opcua_find_similar_nodes`, `opcua_debug_search`, `opcua_discovery_stats`, `opcua_force_discovery`, `opcua_ensure_server_nodes`, `opcua_subscribe`, `opcua_unsubscribe`, `opcua_list_subscriptions`.
- There is **no** OPC-UA method-call tool today, so "method calls" can only apply to a tool added later.
- `opcua_connect` returns "Already connected" based only on the `Client.connected` boolean.
- CLAUDE.md hard rule: gating any tool behind a flag needs explicit sign-off and a README changelog callout.

---

## Part 1 - Read-Only Mode

## Question 1
How should read-only mode be configured?

A) One switch plus an override: `MCP_READ_ONLY=true` enables a built-in safe preset, and `MCP_ALLOWED_TOOLS` (comma-separated) explicitly approves extra tools on top of that preset

B) Allowlist only: `MCP_ALLOWED_TOOLS` lists every tool that is exposed; unset means all tools (no separate read-only switch)

C) Read-only switch only, with a fixed built-in safe set and no per-tool override

D) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 2
What should the default be when nothing is configured?

A) Off: all 18 tools exposed exactly as today (backward compatible, opt-in hardening)

B) On: read-only preset by default, mutating tools must be explicitly approved (breaking change, README changelog callout required)

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 3
The paste explicitly excludes write, disconnect and subscription mutations (`opcua_write`, `opcua_disconnect`, `opcua_subscribe`, `opcua_unsubscribe`). How should the borderline tools be classified in the read-only preset?

A) Allow `opcua_connect` (needed in stdio mode, where nothing connects until it is called) and the discovery tools (`opcua_force_discovery`, `opcua_ensure_server_nodes`), since none of them change server data

B) Allow `opcua_connect`, but exclude `opcua_force_discovery` and `opcua_ensure_server_nodes` because they can put a full address-space walk's load on the server

C) Exclude all three; in stdio mode read-only implies connecting eagerly at startup instead of via `opcua_connect`

D) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 4
How should a direct `tools/call` for a disallowed tool fail?

A) Register every tool, hide disallowed ones with mcp-go's `WithToolFilter`, and reject calls in middleware with an explicit error such as "tool opcua_write is disabled by read-only mode (see MCP_ALLOWED_TOOLS)"

B) Do not register disallowed tools at all, so both `tools/list` and `tools/call` behave as if the tool does not exist (generic "tool not found")

C) Other (please describe after [Answer]: tag below)

[Answer]:  A)

## Question 5
Should an unknown name in the allowlist (for example a typo like `opcua_wrte`) be treated as an error?

A) Fail startup with a clear error listing valid tool names

B) Log a warning and ignore the unknown name

C) Other (please describe after [Answer]: tag below)

[Answer]:  A)

---

## Part 2 - Stale-Session Recovery

## Question 6
Which operations should get the one automatic retry after a reconnect?

A) All operations, including `opcua_write`: a `BadSessionIDInvalid`/`BadServerNotConnected` response means the server rejected the request before executing it, so retrying is safe

B) Read-only operations only (read, browse, node info, type info, server info); writes report the error and ask the caller to retry, to avoid any chance of a double write

C) Other (please describe after [Answer]: tag below)

[Answer]: B)

## Question 7
Reconnecting creates a new underlying client, which drops every live subscription. What should happen to subscriptions after a recovery reconnect?

A) Rebuild them from persisted intent, reusing the existing `SubscriptionManager.rebuild` path the `ReconnectWatcher` already uses

B) Leave them dropped and report it; the user re-subscribes manually

C) Other (please describe after [Answer]: tag below)

[Answer]: A)

## Question 8
How should `opcua_connect` verify real client health when the boolean says "connected"?

A) Do a cheap live probe (read `Server_ServerStatus_State`, i=2259); if it fails with a session or connection error, rebuild the connection

B) Check only gopcua's own connection state (`State() == Connected`) without a network round trip

C) Both: check gopcua state first, then the live probe

D) Other (please describe after [Answer]: tag below)

[Answer]: C)

## Question 9
How should "a server that invalidates a session after initial connection" be tested?

A) Mock-seam unit tests (the existing `mockOpcuaClient`) that return `StatusBadSessionIDInvalid` after the first call, plus a Docker integration test that restarts the test-server container mid-session

B) Mock-seam unit tests only

C) Docker integration test only

D) Other (please describe after [Answer]: tag below)

[Answer]: A)

---

## Part 3 - Process

## Question 10
The previous run's custom-node read/write bugfix is complete but still uncommitted, and it touches the same files (`server.go`, `client.go`). How should it be handled?

A) Commit it to main as its own commit(s) before this run starts, so the new work has a clean base

B) Leave it uncommitted and build on top of it; I will commit it myself

C) Other (please describe after [Answer]: tag below)

[Answer]: A)
 
## Question 11
The previous runs enabled the Security Baseline, Resiliency Baseline and Property-Based Testing extensions. Should this run keep that configuration, and how should approval gates work?

A) Keep all three extensions enabled; stop at each AIDLC approval gate

B) Keep all three extensions enabled; autonomous mode (sane defaults, stop only for genuine blockers), as in Unit 3

C) Change the extension set (describe after [Answer]: tag below)

D) Other (please describe after [Answer]: tag below)

[Answer]:  B)
