# OPC-UA MCP Server

A Model Context Protocol (MCP) server that enables integration between OPC-UA servers and Large Language Models (LLMs). This server provides a bridge between OPC-UA industrial automation systems and AI applications, allowing LLMs to interact with industrial data and control systems.

## Changelog

Notable behavior changes from a recent correctness/robustness hardening pass
over the OPC-UA client, MCP server, and node discovery:

- **`opcua_write` now rejects type-mismatched values.** Previously, a value
  that failed data-type validation against the target node was written
  anyway (only a warning was logged). It now returns an error and the write
  is never sent to the device. The tool's parameters are unchanged - only
  error behavior on invalid input.
- **stdio transport no longer logs to stdout**, even if `SERVER_LOG_OUTPUT`
  is left at its default (`stdout`) or set explicitly - stdout carries the
  MCP JSON-RPC stream in stdio mode, and logs there previously corrupted it.
- **`opcua_read` no longer discards an entire batch on one bad-status node.**
  Each requested node now gets its own result (with its own `status` field);
  previously a single failing node aborted the whole read, discarding
  successfully-read values for every other node in the batch.
- **`opcua_browse`/`opcua_browse_nodes` no longer silently truncate results.**
  A node with more than 1000 references previously lost everything past that
  cap with no error or indication; continuation points are now followed
  automatically.
- **Node discovery no longer wipes its cache before rebuilding it.**
  Concurrent reads (via any `opcua_*` search/lookup tool) during a discovery
  cycle no longer risk seeing a spuriously empty cache. Nodes removed from
  the live server are now actually removed from the search index too -
  previously the index only ever grew.
- The OPC-UA client's connection state is now safe for concurrent access
  from multiple goroutines (the background discovery worker and MCP
  tool-handler calls both touch it).

## Features

- **OPC-UA Client Integration**: Full support for OPC-UA client operations including read, write, browse, and server information
- **Multiple Authentication Methods**: Support for anonymous, username/password, and certificate-based authentication
- **Security Support**: Configurable security policies and modes (None, Basic128Rsa15, Basic256, Basic256Sha256, Aes128_Sha256_RsaOaep)
- **Dual Transport Modes**: Support for both stdio and HTTP streamable transports
- **MCP Tools**: Comprehensive set of tools for OPC-UA operations
- **MCP Resources**: Access to OPC-UA node data and server information as resources
- **Configuration Management**: Environment variable-based configuration using caarlos0/env
- **Docker Support**: Containerized deployment with Docker
- **Test Coverage**: 120+ unit tests across configuration, the OPC-UA client,
  MCP tool handlers, and node discovery, including concurrency (`-race`)
  coverage of the client's connection state and discovery's cache

## Architecture

The project follows Go best practices with a modular structure:

```
├── cmd/                    # Application entrypoints
│   └── opcua-mcp.go       # Main application
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── mcp/              # MCP server implementation
│   └── opcua/            # OPC-UA client implementation
├── pkg/                   # Public packages (if needed)
├── Dockerfile            # Container configuration
├── go.mod                # Go module definition
└── README.md             # This file
```

## Installation

### Prerequisites

- Go 1.25 or later
- Docker (optional, for containerized deployment)

### Build from Source

```bash
# Clone the repository
git clone https://github.com/mwieczorkiewicz/opcua-mcp.git
cd opcua-mcp

# Download dependencies
go mod download

# Build the application
go build -o opcua-mcp ./cmd/opcua-mcp.go
```

### Docker Build

```bash
# Build Docker image
docker build -t opcua-mcp .

# Run container
docker run -p 8080:8080 opcua-mcp
```

## Configuration

The server is configured using environment variables with the following structure. All configuration is loaded using the [caarlos0/env](https://github.com/caarlos0/env) library, which provides automatic parsing and validation.

### Server Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_TRANSPORT` | `stdio` | Transport mode: `stdio` or `http` |
| `SERVER_HTTP_PORT` | `8080` | HTTP port for streamable-http mode |
| `SERVER_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `SERVER_LOG_FORMAT` | `json` | Log format: `json` or `text` |
| `SERVER_LOG_OUTPUT` | `stdout` | Log output: `stdout`, `stderr`, or `file` |
| `SERVER_LOG_FILE` | - | Log file path (required if `LOG_OUTPUT=file`) |
| `SERVER_LOG_ADD_SOURCE` | `false` | Add source file/line info to logs |

### OPC-UA Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OPCUA_ENDPOINT` | `opc.tcp://localhost:4840` | OPC-UA server endpoint |
| `OPCUA_AUTH_MODE` | `anonymous` | Authentication mode: `anonymous`, `username`, `certificate` |
| `OPCUA_USERNAME` | - | Username for username authentication (required if `AUTH_MODE=username`) |
| `OPCUA_PASSWORD` | - | Password for username authentication (required if `AUTH_MODE=username`) |
| `OPCUA_CERT_FILE` | - | Client certificate file path (required if `AUTH_MODE=certificate`) |
| `OPCUA_KEY_FILE` | - | Client private key file path (required if `AUTH_MODE=certificate`) |
| `OPCUA_SERVER_CERT` | - | Server certificate file path |
| `OPCUA_SECURITY_POLICY` | `None` | Security policy: `None`, `Basic128Rsa15`, `Basic256`, `Basic256Sha256`, `Aes128_Sha256_RsaOaep` |
| `OPCUA_SECURITY_MODE` | `None` | Security mode: `None`, `Sign`, `SignAndEncrypt` |
| `OPCUA_REQUEST_TIMEOUT` | `30s` | Request timeout (duration format: `30s`, `1m`, etc.) |
| `OPCUA_SESSION_TIMEOUT` | `60s` | Session timeout (duration format: `60s`, `2m`, etc.) |
| `OPCUA_MAX_RETRIES` | `3` | Maximum retry attempts |
| `OPCUA_RETRY_DELAY` | `1s` | Delay between retries (duration format: `1s`, `500ms`, etc.) |

### MCP Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_NAME` | `OPC-UA MCP Server` | MCP server name |
| `MCP_VERSION` | `1.0.0` | MCP server version |
| `MCP_ENABLE_TOOLS` | `true` | Enable MCP tools |
| `MCP_ENABLE_RESOURCES` | `true` | Enable MCP resources |
| `MCP_ENABLE_PROMPTS` | `false` | Enable MCP prompts |
| `MCP_HTTP_PATH` | `/mcp` | HTTP endpoint path |

### Search and Discovery Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SEARCH_ENABLE_DISCOVERY` | `true` | Enable automatic node discovery |
| `SEARCH_DISCOVERY_INTERVAL` | `30s` | Discovery interval (duration format) |
| `SEARCH_DISCOVERY_ROOT_NODE` | `i=85` | Root node for discovery (Objects folder) |
| `SEARCH_MAX_DISCOVERY_DEPTH` | `10` | Maximum discovery depth |
| `SEARCH_MAX_NODES_PER_BROWSE` | `10000` | Maximum nodes per browse operation |
| `SEARCH_ENABLE_SEARCH` | `true` | Enable search functionality |
| `SEARCH_INDEX_PATH` | `./search_index` | Search index directory path |
| `SEARCH_MAX_RESULTS` | `100` | Maximum search results |
| `SEARCH_MIN_SCORE` | `0.1` | Minimum search score threshold |
| `SEARCH_ENABLE_CACHE` | `true` | Enable caching |
| `SEARCH_CACHE_TTL` | `5m` | Cache time-to-live (duration format) |
| `SEARCH_MAX_CACHE_SIZE` | `10000` | Maximum cache size |

### Configuration Validation

The configuration system includes automatic validation:

- **Transport Mode**: Must be `stdio` or `http`
- **Authentication Mode**: Must be `anonymous`, `username`, or `certificate`
- **Security Policy**: Must be one of the supported OPC-UA security policies
- **Security Mode**: Must be `None`, `Sign`, or `SignAndEncrypt`
- **Required Fields**: Username/password required for username auth, certificates required for certificate auth
- **Duration Formats**: Timeout and interval values support Go duration format (`30s`, `1m30s`, `2h`, etc.)

### Environment Variable Examples

#### Development Environment

```bash
# .env file for development
SERVER_TRANSPORT=stdio
SERVER_LOG_LEVEL=debug
SERVER_LOG_FORMAT=text
OPCUA_ENDPOINT=opc.tcp://localhost:4840
OPCUA_AUTH_MODE=anonymous
SEARCH_ENABLE_DISCOVERY=true
SEARCH_DISCOVERY_INTERVAL=10s
SEARCH_MAX_DISCOVERY_DEPTH=5
```

#### Production Environment

```bash
# Production configuration
SERVER_TRANSPORT=http
SERVER_HTTP_PORT=8080
SERVER_LOG_LEVEL=info
SERVER_LOG_FORMAT=json
SERVER_LOG_OUTPUT=stdout
OPCUA_ENDPOINT=opc.tcp://production-server:4840
OPCUA_AUTH_MODE=username
OPCUA_USERNAME=production_user
OPCUA_PASSWORD=secure_password
OPCUA_SECURITY_POLICY=Basic256
OPCUA_SECURITY_MODE=SignAndEncrypt
OPCUA_REQUEST_TIMEOUT=60s
OPCUA_SESSION_TIMEOUT=300s
OPCUA_MAX_RETRIES=5
OPCUA_RETRY_DELAY=2s
SEARCH_ENABLE_DISCOVERY=true
SEARCH_DISCOVERY_INTERVAL=60s
SEARCH_MAX_DISCOVERY_DEPTH=15
SEARCH_MAX_NODES_PER_BROWSE=50000
SEARCH_ENABLE_CACHE=true
SEARCH_CACHE_TTL=10m
SEARCH_MAX_CACHE_SIZE=50000
```

#### Certificate-based Authentication

```bash
# Certificate authentication setup
OPCUA_AUTH_MODE=certificate
OPCUA_CERT_FILE=/etc/opcua/certs/client.pem
OPCUA_KEY_FILE=/etc/opcua/certs/client.key
OPCUA_SERVER_CERT=/etc/opcua/certs/server.pem
OPCUA_SECURITY_POLICY=Basic256Sha256
OPCUA_SECURITY_MODE=SignAndEncrypt
OPCUA_ENDPOINT=opc.tcp://secure-server:4840
```

### Configuration Best Practices

1. **Environment Separation**: Use different environment files for development, staging, and production
2. **Secret Management**: Never hardcode passwords or certificates in environment files
3. **Logging**: Use structured logging (JSON) in production for better observability
4. **Timeouts**: Set appropriate timeouts based on network conditions and server performance
5. **Discovery**: Adjust discovery intervals based on OPC-UA server size and update frequency
6. **Caching**: Enable caching for better performance, adjust TTL based on data volatility
7. **Security**: Use the highest security policy and mode supported by your OPC-UA server
8. **Monitoring**: Enable debug logging temporarily for troubleshooting, but use info level in production

## Usage

### Stdio Mode

```bash
# Run in stdio mode (default)
./opcua-mcp

# Or with custom configuration
SERVER_TRANSPORT=stdio OPCUA_ENDPOINT=opc.tcp://192.168.1.100:4840 ./opcua-mcp
```

### HTTP Mode

```bash
# Run in HTTP mode
SERVER_TRANSPORT=http SERVER_HTTP_PORT=8080 ./opcua-mcp

# Access the MCP endpoint
curl http://localhost:8080/mcp
```

### Authentication Examples

#### Username/Password Authentication

```bash
OPCUA_AUTH_MODE=username \
OPCUA_USERNAME=admin \
OPCUA_PASSWORD=secret \
OPCUA_ENDPOINT=opc.tcp://server:4840 \
./opcua-mcp
```

#### Certificate Authentication

```bash
OPCUA_AUTH_MODE=certificate \
OPCUA_CERT_FILE=/path/to/client.pem \
OPCUA_KEY_FILE=/path/to/client.key \
OPCUA_SERVER_CERT=/path/to/server.pem \
OPCUA_SECURITY_POLICY=Basic256 \
OPCUA_SECURITY_MODE=SignAndEncrypt \
OPCUA_ENDPOINT=opc.tcp://server:4840 \
./opcua-mcp
```

## MCP Tools

The server provides the following MCP tools:

### `opcua_read`
Read values from OPC-UA nodes.

**Parameters:**
- `node_ids` (required): Comma-separated list of node IDs to read

**Example:**
```json
{
  "node_ids": "ns=2;i=1,ns=2;i=2"
}
```

### `opcua_write`
Write values to OPC-UA nodes.

**Parameters:**
- `node_id` (required): Node ID to write to
- `value` (required): Value to write (JSON format)

**Example:**
```json
{
  "node_id": "ns=2;i=1",
  "value": "42"
}
```

### `opcua_browse`
Browse OPC-UA node hierarchy.

**Parameters:**
- `node_id` (optional): Node ID to browse from (defaults to Objects folder)

**Example:**
```json
{
  "node_id": "i=85"
}
```

### `opcua_node_info`
Get information about an OPC-UA node.

**Parameters:**
- `node_id` (required): Node ID to get information for

**Example:**
```json
{
  "node_id": "ns=2;i=1"
}
```

### `opcua_server_info`
Get OPC-UA server information.

**Parameters:** None

### `opcua_connect`
Connect to OPC-UA server.

**Parameters:** None

### `opcua_disconnect`
Disconnect from OPC-UA server.

**Parameters:** None

### `opcua_get_value`
Get the value of a single variable by node ID. A convenience wrapper over
`opcua_read` for the single-node case.

**Parameters:**
- `node_id` (required): Node ID of the variable to read

### `opcua_browse_nodes`
Recursively browse OPC-UA nodes starting from a given node, up to a depth
limit. Unlike `opcua_browse` (one level only), this walks the hierarchy and
nests each level's children under its parent in the response.

**Parameters:**
- `node_id` (optional, default `i=85`): Node ID to start browsing from
- `max_depth` (optional, default `3`, range `1`-`10`): Maximum depth to browse

### `opcua_get_value_by_name`
Get the value of a variable found via the discovery cache's browse name
index, without needing its node ID.

**Parameters:**
- `browse_name` (required): Browse name of the variable to read

### `opcua_find_similar_nodes`
Find nodes with a browse name similar to the given one, using a tiered
match strategy (exact, then partial, then fuzzy).

**Parameters:**
- `browse_name` (required): Browse name to search for
- `max_results` (optional, default `10`, range `1`-`100`): Maximum results to return

### `opcua_discovery_stats`
Get statistics about the background node discovery cache (total nodes,
depth distribution, and whether discovery/search/caching are enabled).

**Parameters:** None

### `opcua_force_discovery`
Force an immediate discovery refresh instead of waiting for the next
scheduled cycle (`SEARCH_DISCOVERY_INTERVAL`).

**Parameters:** None

### `opcua_debug_search`
Diagnostic tool that reports cache/search-index statistics and runs several
search strategies against a browse name, to help debug why a node isn't
being found.

**Parameters:**
- `browse_name` (required): Browse name to debug search for

### `opcua_ensure_server_nodes`
Diagnostic tool that verifies the standard `Server`/`ServerStatus` nodes are
indexed, forcing a discovery refresh if they aren't.

**Parameters:** None

## MCP Resources

The server provides the following MCP resources:

### `opcua://node/{node_id}`
Access OPC-UA node data.

**Example:** `opcua://node/ns=2;i=1`

### `opcua://server`
OPC-UA server information.

**Example:** `opcua://server`

## Development

### Development Environment Setup

This project includes a complete development environment setup with a Microsoft OPC UA test server for easy testing and debugging.

#### Prerequisites

- Go 1.25 or later
- Docker (for OPC UA test server)
- VS Code (optional, for debugging)

#### Quick Start

1. **Start the OPC UA test server:**
   ```bash
   make start-opcua-server
   ```
   This will start a Microsoft OPC UA test server in a Docker container on `opc.tcp://localhost:4840` using the sample server configuration.

2. **Run the application with the test server:**
   ```bash
   make run-with-test-server
   ```
   This will automatically start the test server, run the application, and stop the server when done.

3. **Stop the test server manually:**
   ```bash
   make stop-opcua-server
   ```

#### VS Code Debugging

The project includes VS Code configuration templates for easy debugging:

1. **Copy the launch configuration:**
   ```bash
   cp .vscode/launch.example.json .vscode/launch.json
   ```

2. **Open VS Code and use the debug configurations:**
   - `Launch OPC UA MCP Server (HTTP)` - Debug with HTTP transport
   - `Launch OPC UA MCP Server (STDIO)` - Debug with stdio transport
   - `Launch OPC UA MCP Server (HTTP with Auth)` - Debug with username authentication

The debug configurations will automatically start the OPC UA test server before debugging and stop it after debugging.

#### Available Makefile Targets

- `make start-opcua-server` - Start Microsoft OPC UA test server (Docker)
- `make stop-opcua-server` - Stop Microsoft OPC UA test server
- `make run-with-test-server` - Run app with test server (auto start/stop)
- `make run-with-server` - Run with custom OPC-UA server
- `make run-with-auth` - Run with username authentication

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with verbose output
go test -v ./...
```

### Code Quality

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Run vet
go vet ./...
```

### Building

```bash
# Build for current platform
go build -o opcua-mcp ./cmd/opcua-mcp.go

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o opcua-mcp-linux ./cmd/opcua-mcp.go

# Build for Windows
GOOS=windows GOARCH=amd64 go build -o opcua-mcp.exe ./cmd/opcua-mcp.go
```

## Docker Deployment

The application is containerized using a multi-stage Docker build with Chainguard's minimal Go image for building and a scratch image for the final runtime.

### Docker Image Details

- **Base Image**: `cgr.dev/chainguard/go` (builder stage)
- **Runtime Image**: `scratch` (minimal, security-focused)
- **Architecture**: Linux AMD64
- **Size**: Optimized for minimal footprint
- **Security**: Uses Chainguard's hardened images

### Build and Run

#### Basic Usage

```bash
# Build image
docker build -t opcua-mcp .

# Run container in stdio mode (default)
docker run opcua-mcp

# Run container in HTTP mode
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  opcua-mcp
```

#### With OPC-UA Server Connection

```bash
# Anonymous authentication
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  -e OPCUA_AUTH_MODE=anonymous \
  opcua-mcp

# Username/password authentication
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  -e OPCUA_AUTH_MODE=username \
  -e OPCUA_USERNAME=admin \
  -e OPCUA_PASSWORD=secret \
  opcua-mcp

# Certificate authentication (with volume mounts)
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

#### Advanced Configuration

```bash
# Full configuration example
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e SERVER_HTTP_PORT=8080 \
  -e SERVER_LOG_LEVEL=debug \
  -e SERVER_LOG_FORMAT=json \
  -e OPCUA_ENDPOINT=opc.tcp://opcua-server:4840 \
  -e OPCUA_AUTH_MODE=username \
  -e OPCUA_USERNAME=admin \
  -e OPCUA_PASSWORD=secret \
  -e OPCUA_REQUEST_TIMEOUT=60s \
  -e OPCUA_SESSION_TIMEOUT=120s \
  -e OPCUA_MAX_RETRIES=5 \
  -e SEARCH_ENABLE_DISCOVERY=true \
  -e SEARCH_DISCOVERY_INTERVAL=60s \
  -e SEARCH_MAX_DISCOVERY_DEPTH=15 \
  -v ./search_index:/search_index \
  opcua-mcp
```

### Docker Compose

#### Basic Setup

```yaml
version: '3.8'
services:
  opcua-mcp:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SERVER_TRANSPORT=http
      - OPCUA_ENDPOINT=opc.tcp://opcua-server:4840
      - OPCUA_AUTH_MODE=anonymous
    depends_on:
      - opcua-server
    volumes:
      - ./search_index:/search_index
```

#### Production Setup with Authentication

```yaml
version: '3.8'
services:
  opcua-mcp:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SERVER_TRANSPORT=http
      - SERVER_LOG_LEVEL=info
      - SERVER_LOG_FORMAT=json
      - OPCUA_ENDPOINT=opc.tcp://opcua-server:4840
      - OPCUA_AUTH_MODE=username
      - OPCUA_USERNAME=${OPCUA_USERNAME}
      - OPCUA_PASSWORD=${OPCUA_PASSWORD}
      - OPCUA_SECURITY_POLICY=Basic256
      - OPCUA_SECURITY_MODE=SignAndEncrypt
      - OPCUA_REQUEST_TIMEOUT=30s
      - OPCUA_SESSION_TIMEOUT=60s
      - SEARCH_ENABLE_DISCOVERY=true
      - SEARCH_DISCOVERY_INTERVAL=30s
      - SEARCH_MAX_DISCOVERY_DEPTH=10
    depends_on:
      - opcua-server
    volumes:
      - opcua-mcp-search:/search_index
      - ./certs:/certs:ro
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/mcp"]
      interval: 30s
      timeout: 10s
      retries: 3

  opcua-server:
    image: mcr.microsoft.com/iotedge/opc-plc:latest
    ports:
      - "4840:4840"
    environment:
      - PLC_SIMULATION_FILE=Boiler1/Boiler1_simulation.json
    restart: unless-stopped

volumes:
  opcua-mcp-search:
```

#### Development Setup with Test Server

```yaml
version: '3.8'
services:
  opcua-mcp:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SERVER_TRANSPORT=http
      - SERVER_LOG_LEVEL=debug
      - OPCUA_ENDPOINT=opc.tcp://opcua-test-server:4840
      - OPCUA_AUTH_MODE=anonymous
      - SEARCH_ENABLE_DISCOVERY=true
      - SEARCH_DISCOVERY_INTERVAL=10s
    depends_on:
      - opcua-test-server
    volumes:
      - ./search_index:/search_index

  opcua-test-server:
    image: mcr.microsoft.com/iotedge/opc-plc:latest
    ports:
      - "4840:4840"
    environment:
      - PLC_SIMULATION_FILE=Boiler1/Boiler1_simulation.json
```

### Environment Variables in Docker

The Docker image includes default environment variables that can be overridden:

```dockerfile
# Default environment variables set in Dockerfile
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
```

### Volume Mounts

Common volume mounts for Docker deployment:

```bash
# Search index persistence
-v ./search_index:/search_index

# Certificate files (read-only)
-v /path/to/certs:/certs:ro

# Log files (if using file output)
-v ./logs:/logs

# Configuration files (if using file-based config)
-v ./config:/config:ro
```

### Health Checks

The application supports health checks for container orchestration:

```yaml
healthcheck:
  test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/mcp"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 40s
```

### Security Considerations

- **Minimal Base Image**: Uses `scratch` for minimal attack surface
- **Read-only Certificates**: Mount certificate volumes as read-only
- **Non-root User**: Consider running as non-root user in production
- **Network Security**: Use Docker networks for service isolation
- **Secret Management**: Use Docker secrets or external secret management for sensitive data

## Security Considerations

- **Network Security**: Use secure networks for OPC-UA connections
- **Certificate Management**: Properly manage client and server certificates
- **Authentication**: Use strong authentication methods in production
- **Encryption**: Enable encryption for sensitive data transmission
- **Access Control**: Implement proper access controls for OPC-UA servers

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Submit a pull request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [mcp-go](https://github.com/mark3labs/mcp-go) - MCP implementation for Go
- [gopcua/opcua](https://github.com/gopcua/opcua) - OPC-UA client library for Go
- [caarlos0/env](https://github.com/caarlos0/env) - Environment variable parsing for Go
- [mwieczorkiewicz/opcua_exporter](https://github.com/mwieczorkiewicz/opcua_exporter) - Reference implementation for OPC-UA client patterns

## Support

For issues and questions:
- Create an issue on GitHub
- Check the documentation
- Review the test cases for usage examples
