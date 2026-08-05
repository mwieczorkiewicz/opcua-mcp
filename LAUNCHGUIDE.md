# OPC-UA MCP Server

## Tagline
Read, write, browse, and subscribe to live OPC-UA industrial data from any MCP client.

## Description
opcua-mcp is a Go-based MCP server that bridges LLM clients (Claude, and any other MCP-compatible
client) to OPC-UA industrial automation servers - the standard protocol used by PLCs, SCADA
systems, and factory/process equipment. It exposes read, write, browse, discovery, search, and
push-based subscription operations as MCP tools, backed by a persistent on-disk cache and a
background-built, full-text-searchable index of the connected server's address space.

It's built for industrial/OT engineers, integrators, and developers who want an LLM to inspect or
operate an OPC-UA server conversationally - e.g. "what's the current value of the mixer
temperature sensor," "browse the Objects folder," or "subscribe me to every node under
Line3/Pumps and alert me on change" - without hand-writing OPC-UA client code for each query.
Runs over stdio (for local/desktop MCP clients) or HTTP (for remote/hosted deployments), and
supports anonymous, username/password, or certificate authentication against the OPC-UA server
itself.

## Setup Requirements
- `OPCUA_ENDPOINT` (required): The OPC-UA server endpoint to connect to, e.g.
  `opc.tcp://your-plc-host:4840`. Defaults to `opc.tcp://localhost:4840`, which only works if
  you're running a local/test OPC-UA server.
- `OPCUA_AUTH_MODE` (optional): `anonymous` (default), `username`, or `certificate`.
- `OPCUA_USERNAME` / `OPCUA_PASSWORD` (optional): required if `OPCUA_AUTH_MODE=username`.
- `OPCUA_CERT_FILE` / `OPCUA_KEY_FILE` (optional): required if `OPCUA_AUTH_MODE=certificate`.
- `OPCUA_SECURITY_POLICY` / `OPCUA_SECURITY_MODE` (optional): OPC-UA transport security, default
  `None` / `None`. Set to e.g. `Basic256Sha256` / `SignAndEncrypt` for an encrypted connection.
- `SERVER_TRANSPORT` (optional): `stdio` (default, for local MCP clients) or `http` (for
  remote/hosted deployments, listens on `SERVER_HTTP_PORT`, default `8080`).
- `DO_NOT_TRACK` / `OPCUA_MCP_TELEMETRY` (optional): set `DO_NOT_TRACK=1` or
  `OPCUA_MCP_TELEMETRY=false` to opt out of anonymous usage telemetry (on by default; never
  collects node IDs, endpoint URLs, node values, or credentials - see docs/telemetry.md).

Full configuration reference (30+ variables covering discovery, search, caching, and the
persistent store) is in the project README.

## Category
Developer Tools

## Features
- Read one or more OPC-UA node values in a single call, served from a live subscription cache
  when available, or fetched live otherwise
- Write values to nodes, with type validation against the node's declared data type before the
  write is sent
- Browse the address space one level at a time, or recursively to a depth limit
- Look up nodes by browse name instead of raw node ID, via a background-built search index
- Fuzzy/partial-match search across node browse names
- Subscribe to push-based live updates on one or more nodes; subscriptions persist across server
  restarts and are automatically re-established on reconnect
- List, inspect, and cancel active subscriptions
- Explicit connect/disconnect control, useful in stdio mode where the OPC-UA connection is
  established lazily
- Fetch node metadata - data type, access level, and other attributes
- Fetch OPC-UA server metadata (name, URIs, build info)
- Background discovery that continuously walks the address space and keeps the cache and search
  index in sync via mark-and-sweep, so deleted nodes disappear and new nodes appear automatically
- Trigger an immediate discovery refresh on demand instead of waiting for the next cycle
- Diagnostics tools for troubleshooting why a specific node isn't showing up in search results
- On-disk persistent caching (bbolt) for reads, browse results, and type info, with automatic
  invalidation on writes, so repeat lookups don't round-trip to the device
- Anonymous, username/password, or certificate authentication, with configurable OPC-UA security
  policy and mode
- Works over stdio for local/desktop MCP clients or HTTP for remote/hosted deployments

## Getting Started
- "Connect to my OPC-UA server and show me the server info"
- "Browse the Objects folder and list what's under it"
- "What's the current value of node ns=2;i=1002?"
- "Find every node with 'temperature' in its name"
- "Subscribe me to updates on the mixer speed and tank level nodes every 2 seconds"
- "Write the value 75 to the setpoint node, but validate the type first"
- Tool: `opcua_connect` / `opcua_disconnect` - establish or tear down the OPC-UA connection
  explicitly (mainly relevant in stdio mode, where the connection is lazy)
- Tool: `opcua_browse` / `opcua_browse_nodes` - list a node's children, or recursively browse a
  subtree
- Tool: `opcua_read` / `opcua_get_value` / `opcua_get_value_by_name` - read node values by ID or
  by browse name
- Tool: `opcua_write` - write a value to a node, validated against its declared type
- Tool: `opcua_find_similar_nodes` - fuzzy-match a browse name against the discovery index
- Tool: `opcua_subscribe` / `opcua_unsubscribe` / `opcua_list_subscriptions` - manage push-based
  live updates

## Tags
opc-ua, opcua, industrial-automation, iot, industrial-iot, plc, scada, manufacturing, automation,
factory, ot, edge, sensors, telemetry, real-time-data, data-acquisition, protocol-bridge, browse,
search, subscriptions, caching, discovery, go, golang, self-hosted, stdio, http, model-context-protocol

## Documentation URL
https://github.com/mwieczorkiewicz/opcua-mcp#readme

## Health Check URL
Not applicable - opcua-mcp is self-hosted, not a centrally hosted service. In HTTP mode, the
running instance exposes its MCP endpoint at `http://<your-host>:8080/mcp`, which can be used as
a liveness probe (see `docs/deployment.md`). There is no dedicated `/health` route and no
Anthropic-hosted instance to link to.
