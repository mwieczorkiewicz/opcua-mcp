# Deployment

## Image

Multi-stage build: `cgr.dev/chainguard/go` (builder) →
`cgr.dev/chainguard/static` (runtime). No shell, no package manager, nothing
but the static binary and a CA certificate bundle (needed for outbound TLS,
e.g. `internal/telemetry`'s calls to Aptabase) - minimal attack surface,
linux/amd64 and linux/arm64. Runs as the image's built-in nonroot user rather
than root, which is why `WORKDIR` in the Dockerfile is `/home/nonroot` rather
than `/` - `STORE_DB_PATH`/`SEARCH_INDEX_PATH` default to relative paths
created under `WORKDIR` at runtime.

```bash
docker build -t opcua-mcp .
docker run -p 8080:8080 -e SERVER_TRANSPORT=http opcua-mcp
```

Default `ENV` values baked into the image (all overridable with `-e`):

```dockerfile
SERVER_TRANSPORT=stdio
SERVER_HTTP_PORT=8080
SERVER_LOG_LEVEL=info
OPCUA_ENDPOINT=opc.tcp://localhost:4840
OPCUA_AUTH_MODE=anonymous
OPCUA_SECURITY_POLICY=None
OPCUA_SECURITY_MODE=None
OPCUA_REQUEST_TIMEOUT=30s
OPCUA_SESSION_TIMEOUT=60s
OPCUA_MAX_RETRIES=3
OPCUA_RETRY_DELAY=1s
MCP_HTTP_PATH=/mcp
```

## Connecting to a real server

```bash
# anonymous
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  opcua-mcp

# username/password
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  -e OPCUA_AUTH_MODE=username \
  -e OPCUA_USERNAME=admin \
  -e OPCUA_PASSWORD=secret \
  opcua-mcp

# certificate, signed + encrypted
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  -e OPCUA_AUTH_MODE=certificate \
  -e OPCUA_CERT_FILE=/certs/client.pem \
  -e OPCUA_KEY_FILE=/certs/client.key \
  -e OPCUA_SERVER_CERT=/certs/server.pem \
  -e OPCUA_SECURITY_POLICY=Basic256 \
  -e OPCUA_SECURITY_MODE=SignAndEncrypt \
  -v /path/to/certs:/certs:ro \
  opcua-mcp
```

## Persistence

Both the search index and the bbolt store should be mounted as volumes so
discovery/cache/subscription state survives a container restart:

```bash
-v ./search_index:/search_index
-v ./mcp_opcua_store.db:/mcp_opcua_store.db
```

Without the second mount, subscriptions won't survive a restart (they'll be
lost, not silently wrong) and every cache lookup starts cold.

## Docker Compose dev stack

`docker-compose.yml` is a ready-to-use dev/test stack: a Microsoft OPC-UA
test server, `opcua-mcp` pulled from `ghcr.io/mwieczorkiewicz/opcua-mcp` in
HTTP mode against it, and a `cloudflared` sidecar that publishes `opcua-mcp`
over a public HTTPS tunnel.

```bash
make compose-build       # pull the pinned opcua-mcp image
make compose-up-server   # start just the test server, detached
make compose-down        # stop everything
```

### Testing with a Claude custom connector

Claude's custom connector URL field (Settings → Connectors → Add custom
connector) requires a publicly reachable `https://` endpoint - it rejects
`http://localhost`, since the connection is made from Anthropic's
infrastructure, not your machine. The `cloudflared` service handles this
with a free, no-account "quick tunnel":

```bash
make compose-up        # starts opcua-server, opcua-mcp (HTTP), and the tunnel
make connector-url     # prints the URL to paste into Claude, e.g. https://xyz.trycloudflare.com/mcp
```

Paste the printed URL into Claude's "Add custom connector" dialog. The
tunnel URL is ephemeral and regenerates on every `cloudflared` restart -
re-run `make connector-url` after any `make compose-up`.

> **Security**: while this stack is running, that URL is reachable by
> anyone on the internet who has it, and `opcua-mcp` has no authentication
> of its own. Only run it for active local testing, and stop the stack
> (`make compose-down`) when done.

## Health checks

```yaml
healthcheck:
  test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/mcp"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

Only meaningful in HTTP mode - stdio mode has no listening port to probe.
