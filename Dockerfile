FROM cgr.dev/chainguard/go AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# APTABASE_APP_KEY is injected at build time (see docker-build.yml, which
# passes it from the APTABASE_APP_KEY repository secret) rather than being
# baked into internal/telemetry/client.go's source default. This ARG only
# exists in this discarded builder stage - the final runtime image below has
# no build history of its own, so the key value is never present in the
# pushed image's layers/metadata.
ARG APTABASE_APP_KEY=A-DEV-0000000000
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry.appKey=${APTABASE_APP_KEY}" \
    -o opcua-mcp ./cmd/opcua-mcp.go

# chainguard/static (not `scratch`) - scratch has no CA certificate bundle at
# all, so any outbound TLS call (e.g. internal/telemetry's HTTPS POST to
# Aptabase) fails verification with "x509: certificate signed by unknown
# authority". chainguard/static is Chainguard's distroless-equivalent: still
# no shell, no package manager, nothing but the binary and a CA bundle - same
# minimal attack surface as scratch, just with working TLS.
FROM cgr.dev/chainguard/static:latest
# chainguard/static runs as its built-in nonroot user (uid 65532) rather than
# root, unlike `scratch` - so WORKDIR must be a directory that user actually
# owns (STORE_DB_PATH/SEARCH_INDEX_PATH default to relative paths, created
# under WORKDIR at runtime) rather than `/`, which nonroot can't write to.
WORKDIR /home/nonroot
COPY --from=builder /build/opcua-mcp /opcua-mcp

# Default environment variables
ENV SERVER_TRANSPORT=stdio
ENV SERVER_HTTP_PORT=8080
ENV SERVER_LOG_LEVEL=info
ENV OPCUA_ENDPOINT=opc.tcp://localhost:4840
ENV OPCUA_AUTH_MODE=anonymous
ENV OPCUA_SECURITY_POLICY=None
ENV OPCUA_SECURITY_MODE=None
ENV OPCUA_REQUEST_TIMEOUT=30s
ENV OPCUA_SESSION_TIMEOUT=60s
ENV OPCUA_MAX_RETRIES=3
ENV OPCUA_RETRY_DELAY=1s
ENV MCP_NAME="OPC-UA MCP Server"
ENV MCP_VERSION=1.0.0
ENV MCP_ENABLE_TOOLS=true
ENV MCP_ENABLE_RESOURCES=true
ENV MCP_ENABLE_PROMPTS=false
ENV MCP_HTTP_PATH=/mcp

ENTRYPOINT ["/opcua-mcp"]
