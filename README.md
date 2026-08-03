# OPC-UA MCP Server

An MCP server that lets an LLM read, write, browse, search, and subscribe to
live data on an OPC-UA industrial automation server - over stdio or HTTP,
with a persistent cache and a searchable index of the address space built in.

https://github.com/user-attachments/assets/0b676e6e-17ce-42f5-918f-9a615e939008

## Quick start

The fastest way to see it working end-to-end, using the bundled Microsoft
OPC-UA test server and a public tunnel Claude can reach:

```bash
git clone https://github.com/mwieczorkiewicz/opcua-mcp.git
cd opcua-mcp
make compose-up        # starts a test OPC-UA server, opcua-mcp, and a public HTTPS tunnel
make connector-url      # prints a URL like https://xyz.trycloudflare.com/mcp
```

Paste that URL into Claude at **Settings → Connectors → Add custom
connector**, then ask it to browse the server or read a value. Stop with
`make compose-down` when you're done - see
[docs/deployment.md](docs/deployment.md) for what that tunnel exposes and
how to run against your own OPC-UA server instead.

### Building and running locally

```bash
go build -o opcua-mcp ./cmd/opcua-mcp.go

# stdio (default) - no OPC-UA connection until the client calls opcua_connect
./opcua-mcp

# HTTP - connects eagerly at startup
SERVER_TRANSPORT=http OPCUA_ENDPOINT=opc.tcp://localhost:4840 ./opcua-mcp
```

Requires Go 1.26+ and, optionally, Docker for the test server / containerized
deployment.

## What it does

- **Read / write** node values, with type validation on writes so a
  mismatched value is rejected before it reaches the device.
- **Browse** the address space one level at a time or recursively, and look
  nodes up by name instead of by node ID.
- **Subscribe** to push-based live updates - subscriptions persist across
  restarts and are automatically re-established on reconnect.
- **Cache** reads, browse results, and type info on disk (bbolt), so repeat
  lookups don't round-trip to the device; writes invalidate the relevant
  entry automatically.
- **Discover and search** the address space in the background, indexed with
  Bleve for fuzzy/partial browse-name lookups.
- **Anonymous, username/password, or certificate auth**, with configurable
  OPC-UA security policy and mode.

See [docs/architecture.md](docs/architecture.md) for how the caching layer,
subscription manager, and discovery index fit together.

## MCP tools

| Tool | Description |
|---|---|
| `opcua_read` | Read one or more node values. Subscribed nodes are served from the live cache; others go live unless `max_age_ms` allows a cached value. |
| `opcua_write` | Write a value to a node. Validates the value's type against the node before writing. |
| `opcua_get_value` | Read a single node's value - a convenience wrapper over `opcua_read`. |
| `opcua_get_value_by_name` | Read a value by browse name instead of node ID, via the discovery index. |
| `opcua_browse` | List a node's immediate children. |
| `opcua_browse_nodes` | Recursively browse from a node up to a depth limit, nesting children under their parent. |
| `opcua_node_info` | Get a node's metadata (data type, access level, etc.). |
| `opcua_find_similar_nodes` | Fuzzy-match browse names against the discovery index. |
| `opcua_subscribe` | Start push-based updates for one or more nodes at a given interval. |
| `opcua_unsubscribe` | Cancel a subscription, by ID or by naming one of its nodes. |
| `opcua_list_subscriptions` | List active subscriptions. |
| `opcua_connect` / `opcua_disconnect` | Manage the connection explicitly (mainly relevant in stdio mode). |
| `opcua_server_info` | Get OPC-UA server metadata. |
| `opcua_discovery_stats` | Stats on the background discovery cache (node count, depth distribution, enabled flags). |
| `opcua_force_discovery` | Trigger an immediate discovery refresh instead of waiting for the next cycle. |
| `opcua_debug_search` / `opcua_ensure_server_nodes` | Diagnostics for troubleshooting why a node isn't showing up in search. |

## MCP resources

| Resource | Description |
|---|---|
| `opcua://node/{node_id}` | Node data, e.g. `opcua://node/ns=2;i=1`. Accepts a comma-separated list for multiple nodes. |
| `opcua://server` | OPC-UA server information. |

## Configuration

Configuration is loaded (via [viper](https://github.com/spf13/viper)) from
three sources, in ascending order of precedence:

1. Built-in defaults (shown in the tables below).
2. An optional config file - TOML, YAML, JSON, or any other format viper
   supports. By default `./config.{yaml,yml,toml,json,...}` is read if
   present; point at an explicit path with `CONFIG_FILE=/path/to/config.toml`.
   A config file is entirely optional - env vars alone are still enough.
3. Environment variables (`SERVER_*`, `OPCUA_*`, `MCP_*`, `SEARCH_*`, `STORE_*`) - **always win** over the config file, so existing env-var-only deployments keep working unchanged.

A config file mirrors the env var names, lowercased and nested under each
prefix, e.g. `SERVER_HTTP_PORT` becomes:

```yaml
server:
  http_port: "8080"
```

### Server

| Variable | Default | Description |
|---|---|---|
| `SERVER_TRANSPORT` | `stdio` | `stdio` or `http` |
| `SERVER_HTTP_PORT` | `8080` | Port for HTTP transport |
| `SERVER_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `SERVER_LOG_FORMAT` | `json` | `json` or `text` |
| `SERVER_LOG_OUTPUT` | `stdout` | `stdout`, `stderr`, or `file` (forced to `stderr` in stdio mode, since stdout carries the MCP stream) |
| `SERVER_LOG_FILE` | - | Log file path, required if `SERVER_LOG_OUTPUT=file` |
| `SERVER_LOG_ADD_SOURCE` | `false` | Add source file/line to log entries |

### OPC-UA connection

| Variable | Default | Description |
|---|---|---|
| `OPCUA_ENDPOINT` | `opc.tcp://localhost:4840` | Server endpoint |
| `OPCUA_AUTH_MODE` | `anonymous` | `anonymous`, `username`, or `certificate` |
| `OPCUA_USERNAME` / `OPCUA_PASSWORD` | - | Required if `AUTH_MODE=username` |
| `OPCUA_CERT_FILE` / `OPCUA_KEY_FILE` | - | Required if `AUTH_MODE=certificate` |
| `OPCUA_SERVER_CERT` | - | Server certificate file path |
| `OPCUA_SECURITY_POLICY` | `None` | `None`, `Basic128Rsa15`, `Basic256`, `Basic256Sha256`, `Aes128_Sha256_RsaOaep` |
| `OPCUA_SECURITY_MODE` | `None` | `None`, `Sign`, `SignAndEncrypt` |
| `OPCUA_REQUEST_TIMEOUT` | `30s` | Per-request timeout |
| `OPCUA_SESSION_TIMEOUT` | `60s` | Session timeout |
| `OPCUA_MAX_RETRIES` | `3` | Connection retry attempts |
| `OPCUA_RETRY_DELAY` | `1s` | Delay between retries |

### MCP

| Variable | Default | Description |
|---|---|---|
| `MCP_NAME` | `OPC-UA MCP Server` | Server name reported to clients |
| `MCP_VERSION` | `1.0.0` | Server version reported to clients |
| `MCP_ENABLE_TOOLS` | `true` | Enable tools |
| `MCP_ENABLE_RESOURCES` | `true` | Enable resources |
| `MCP_ENABLE_PROMPTS` | `false` | Enable prompts |
| `MCP_HTTP_PATH` | `/mcp` | HTTP endpoint path |

### Discovery and search

| Variable | Default | Description |
|---|---|---|
| `SEARCH_ENABLE_DISCOVERY` | `true` | Enable background node discovery |
| `SEARCH_DISCOVERY_INTERVAL` | `30s` | How often to re-crawl the address space |
| `SEARCH_DISCOVERY_ROOT_NODE` | `i=85` | Root node to crawl from (Objects folder) |
| `SEARCH_MAX_DISCOVERY_DEPTH` | `10` | Maximum crawl depth |
| `SEARCH_MAX_NODES_PER_BROWSE` | `10000` | Cap on nodes returned per browse call |
| `SEARCH_ENABLE_SEARCH` | `true` | Enable the Bleve search index |
| `SEARCH_INDEX_PATH` | `./search_index` | Search index directory |
| `SEARCH_MAX_RESULTS` | `100` | Max results per search |
| `SEARCH_MIN_SCORE` | `0.1` | Minimum match score |
| `SEARCH_ENABLE_CACHE` | `true` | Master switch for read-through caching. `false` makes every `opcua_read`/`opcua_write`/`opcua_browse_nodes` call go live, matching pre-cache behavior exactly |

### Persistent store

Backs read-through caching and subscription persistence with an on-disk
bbolt database.

| Variable | Default | Description |
|---|---|---|
| `STORE_DB_PATH` | `mcp_opcua_store.db` | Database file path |
| `STORE_OPEN_TIMEOUT` | `5s` | How long to wait for the file lock on open |
| `STORE_TYPEINFO_TTL` | `24h` | Freshness window for cached type info |
| `STORE_BROWSE_TTL` | `5m` | Freshness window for cached browse results |
| `STORE_BATCH_WINDOW` | `25ms` | How often subscription notifications flush to the store |
| `STORE_BATCH_MAX_ITEMS` | `250` | Max notifications flushed per batch |
| `STORE_NOTIFY_CHAN_BUFFER` | `1024` | Buffer size for incoming subscription notifications |

If the store fails to open (e.g. a stale lock from a prior ungraceful
shutdown, or a read-only filesystem), the server logs a warning and keeps
running with caching forced off and subscription tools returning an error -
every other tool is unaffected.

## Telemetry

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

**Collected, always aggregated and bucketed - never per-event:**
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

**Never collected, under any circumstance:**
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

### Opting out

| Variable | Effect |
|---|---|
| `DO_NOT_TRACK=1` | Disables telemetry - the cross-project community convention ([consoledonottrack.com](https://consoledonottrack.com)) |
| `OPCUA_MCP_TELEMETRY=false` | Disables telemetry - this project's own switch |

Either one is enough on its own; both accept `1`/`true`/`yes`/`on` and
`0`/`false`/`no`/`off` (case-insensitive), and `DO_NOT_TRACK` takes
precedence if both are set with conflicting values.

## Docker

```bash
docker build -t opcua-mcp .
docker run -p 8080:8080 -e SERVER_TRANSPORT=http -e OPCUA_ENDPOINT=opc.tcp://your-server:4840 opcua-mcp
```

Multi-stage build on Chainguard's minimal Go image, running from `scratch` -
no shell, small attack surface. Mount `./search_index` and
`./mcp_opcua_store.db` as volumes to persist discovery/cache/subscription
state across restarts. Full auth-mode examples, the Compose dev stack, and
the Claude-connector tunnel setup are in
[docs/deployment.md](docs/deployment.md).

## Development

```bash
make start-opcua-server      # Microsoft OPC-UA test server in Docker
make run-with-test-server    # run the app against it (auto start/stop)

go test ./...                # unit tests
go test -race ./...
make test-integration        # real Subscribe/reconnect/cache behavior via testcontainers-go (needs Docker)
```

VS Code launch configs are in `.vscode/launch.example.json` - copy to
`.vscode/launch.json` to get stdio/HTTP/auth debug targets that start and
stop the test server automatically. `make help` lists every available
target.

Tests are table-driven and mock the OPC-UA client at the `opcuaClient`
interface seam (`internal/opcua/mock_client_test.go`) rather than against a
live/simulated server - see [docs/architecture.md](docs/architecture.md) for
how the pieces being tested fit together, and
[docs/COMMIT_CONVENTION.md](docs/COMMIT_CONVENTION.md) for this repo's
commit message format.

## Contributing

Fork it, make your changes, open a PR - see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
