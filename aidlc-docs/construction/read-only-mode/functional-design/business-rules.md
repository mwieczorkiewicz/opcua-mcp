# Functional Design - read-only-mode

## Components

- `config.MCPConfig.ReadOnly bool` (`mcp.read_only`, env `MCP_READ_ONLY`, default false)
- `config.MCPConfig.AllowedTools string` (`mcp.allowed_tools`, env `MCP_ALLOWED_TOOLS`, default "")
- `internal/mcp/tool_policy.go`:
  - `readOnlyTools` - fixed set of tool names classified read-only (the preset)
  - `newToolPolicy(readOnly bool, allowedTools string, registered []string) (*toolPolicy, error)`
  - `(*toolPolicy).allowed(name string) bool`
  - `(*Server).toolPolicyFilter` (mcp-go `ToolFilterFunc`) and `(*Server).toolPolicyMiddleware` (`ToolHandlerMiddleware`)

## Business Rules

| ID | Rule |
|---|---|
| BR-1 | `readOnly == false` => `allowed(name)` is true for every name. |
| BR-2 | `readOnly == true` => `allowed(name)` iff `name` is in `readOnlyTools` or in the parsed allowlist. |
| BR-3 | `readOnlyTools` lists the 14 non-mutating tools explicitly; a tool not in it is denied in read-only mode (deny by default). |
| BR-4 | Allowlist parsing: split on `,`, trim spaces, drop empties. Any remaining name not in `registered` is a startup error listing the valid names, sorted. |
| BR-5 | Policy is built in `SetupTools()` after all tools are registered, from the actual registered names, so the validation set can never drift from the tool list. |
| BR-6 | Middleware order: telemetry is registered first (outermost), policy second, so denied calls are still counted. |
| BR-7 | Denial result: `mcp.NewToolResultError("tool <name> is disabled in read-only mode (MCP_READ_ONLY=true); add it to MCP_ALLOWED_TOOLS to enable it")`. |
| BR-8 | Allowlist set with `readOnly == false`: warn once at startup, policy behaves as BR-1. |

## Testable Properties (PBT-01)

| Property | Category | Test |
|---|---|---|
| P1: for any subset A of registered tools, allowed set == readOnlyTools U A in read-only mode, and == all tools otherwise | Oracle (set model) | `TestToolPolicyMatchesSetModel` (rapid) |
| P2: the filter's output is a subset of its input and contains exactly the allowed tools | Invariant | same test, applied to the filter |
| P3: parsing an allowlist string is order-, whitespace- and duplicate-insensitive; formatting the parsed set and parsing it again gives the same set | Idempotence | `TestAllowlistParseIdempotent` (rapid) |
| P4: no mutating tool (`opcua_write`, `opcua_disconnect`, `opcua_subscribe`, `opcua_unsubscribe`) is allowed in read-only mode unless it is in the allowlist | Invariant (business rule) | covered by P1 plus explicit example test |

Example tests: tools/list hides denied tools and a direct tools/call is rejected without running the handler, both through the real mcp-go JSON-RPC dispatcher (`HandleMessage`).
