# Requirements - Read-Only Mode + Stale-Session Recovery

## Intent Analysis

- **User request**: (1) configurable read-only mode with an explicit tool allowlist, hiding disallowed tools from `tools/list` and rejecting direct `tools/call`; (2) recovery from `StatusBadSessionIDInvalid` / `StatusBadServerNotConnected` via a serialized reconnect with the configured security options and one retry, plus a real health check in `opcua_connect`.
- **Request type**: Enhancement (1) and bug fix (2)
- **Scope**: Multiple components (`internal/config`, `internal/mcp`, `internal/opcua`, README)
- **Complexity**: Moderate
- **Depth**: Standard
- **Answers**: `requirement-verification-questions-readonly-session-recovery.md` (Q1 A, Q2 A, Q3 A, Q4 A, Q5 A, Q6 B, Q7 A, Q8 C, Q9 A, Q10 A, Q11 B)

## Functional Requirements - Read-Only Mode (Unit 1)

| ID | Requirement |
|---|---|
| RO-1 | New config `MCP_READ_ONLY` (bool, default `false`) and `MCP_ALLOWED_TOOLS` (comma-separated tool names, default empty), loaded through `internal/config` like every other `MCP_*` setting (env, config file, defaults). |
| RO-2 | With `MCP_READ_ONLY=false` (default) all 18 tools are exposed exactly as today. Backward compatible. |
| RO-3 | With `MCP_READ_ONLY=true`, exposed tools = read-only preset + `MCP_ALLOWED_TOOLS`. |
| RO-4 | Read-only preset = every tool except `opcua_write`, `opcua_disconnect`, `opcua_subscribe`, `opcua_unsubscribe` (Q3 A: `opcua_connect`, `opcua_force_discovery`, `opcua_ensure_server_nodes` stay allowed). Any tool added later is excluded from the preset unless it is explicitly classified as read-only (deny by default), which covers a future method-call tool. |
| RO-5 | Disallowed tools are hidden from `tools/list` (mcp-go `WithToolFilter`). |
| RO-6 | A direct `tools/call` for a disallowed tool is rejected server-side in tool middleware with an error result naming the tool and the setting that controls it; the handler never runs (Q4 A). |
| RO-7 | An unknown name in `MCP_ALLOWED_TOOLS` fails startup with an error listing valid tool names (Q5 A). Names are trimmed; empty entries are ignored. |
| RO-8 | `MCP_ALLOWED_TOOLS` set while `MCP_READ_ONLY=false` logs a warning that it has no effect (self-resolved ambiguity: allowlist only narrows read-only mode). |
| RO-9 | Denied calls still count in telemetry (`tool_calls.<name>`, `errors.<kind>`); no new telemetry fields. |
| RO-10 | README documents both variables and carries a changelog callout (CLAUDE.md hard rule on gating tools). |

## Functional Requirements - Stale-Session Recovery (Unit 2)

| ID | Requirement |
|---|---|
| SR-1 | A call failing with `StatusBadSessionIDInvalid` or `StatusBadServerNotConnected` (anywhere in the error chain) while the client believes it is connected is a stale session. |
| SR-2 | On a stale session, the client tears down the old connection and reconnects using the same `Connect` path (auth mode, certificates, security policy/mode, endpoint selection). |
| SR-3 | Reconnects are serialized: concurrent callers that hit the same stale connection trigger exactly one reconnect; the others reuse its result. |
| SR-4 | Read-only operations (`Read`, `Browse`, `GetNodeClass`, `GetNodeDataType`, `GetNodeValueRank`, `GetNodeArrayDimensions`, `GetNodeAccessLevel`, `GetNodeUserAccessLevel`, and the composites built on them) retry exactly once after a successful reconnect (Q6 B). |
| SR-5 | `Write` does not retry: on a stale session it reconnects and returns an error stating the write was rejected and not applied, and should be retried (Q6 B). |
| SR-6 | A batch `Read` that fails with a stale session must not fall into the per-node isolation fallback (which would mask the error as per-node bad statuses). |
| SR-7 | After a recovery reconnect, subscriptions are rebuilt from persisted intent (Q7 A). |
| SR-8 | A user-initiated `opcua_disconnect` is never undone by recovery or by the `ReconnectWatcher`; only connections the client still considers live are recovered. |
| SR-9 | `opcua_connect` checks real health when already connected: gopcua connection state first, then a live probe read of `Server_ServerStatus_State` (i=2259); if either fails, the connection is rebuilt (Q8 C). |

## Non-Functional Requirements

| ID | Requirement |
|---|---|
| NFR-1 | No new direct dependencies. |
| NFR-2 | All reconnect and probe calls are bounded by `OPCUA_REQUEST_TIMEOUT` (RESILIENCY-10). |
| NFR-3 | Recovery failure degrades to returning the original error with the recovery failure attached; it never panics or hangs. |
| NFR-4 | Logs never include values read or written; reconnect events are logged at warn level with the operation name only. |
| NFR-5 | `go build`, `go vet`, `gofmt`, `golint`, `go test -race ./...` clean; integration tests pass under `-tags=integration`. |
| NFR-6 | Tests: mock-seam unit tests, property-based tests (PBT-01/03/04), and a Docker integration test that invalidates the session after the initial connection (Q9 A). |

## Extension Compliance Notes

- SECURITY-08 (function-level authorization enforced server-side, never client-side hiding only): RO-5 + RO-6.
- SECURITY-15 (fail closed): RO-4 deny-by-default for unclassified tools, RO-7 startup failure.
- RESILIENCY-10 (timeouts, graceful degradation): NFR-2, NFR-3.
- PBT: properties identified per unit in functional design.
