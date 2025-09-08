# OPC-UA MCP Server

A Model Context Protocol (MCP) server that enables integration between OPC-UA servers and Large Language Models (LLMs). This server provides a bridge between OPC-UA industrial automation systems and AI applications, allowing LLMs to interact with industrial data and control systems.

## Features

- **OPC-UA Client Integration**: Full support for OPC-UA client operations including read, write, browse, and server information
- **Multiple Authentication Methods**: Support for anonymous, username/password, and certificate-based authentication
- **Security Support**: Configurable security policies and modes (None, Basic128Rsa15, Basic256, Basic256Sha256, Aes128_Sha256_RsaOaep)
- **Dual Transport Modes**: Support for both stdio and HTTP streamable transports
- **MCP Tools**: Comprehensive set of tools for OPC-UA operations
- **MCP Resources**: Access to OPC-UA node data and server information as resources
- **Configuration Management**: Environment variable-based configuration using caarlos0/env
- **Docker Support**: Containerized deployment with Docker
- **Comprehensive Testing**: Unit tests with reasonable coverage

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

The server is configured using environment variables with the following structure:

### Server Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_TRANSPORT` | `stdio` | Transport mode: `stdio` or `http` |
| `SERVER_HTTP_PORT` | `8080` | HTTP port for streamable-http mode |
| `SERVER_LOG_LEVEL` | `info` | Log level |

### OPC-UA Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OPCUA_ENDPOINT` | `opc.tcp://localhost:4840` | OPC-UA server endpoint |
| `OPCUA_AUTH_MODE` | `anonymous` | Authentication mode: `anonymous`, `username`, `certificate` |
| `OPCUA_USERNAME` | - | Username for username authentication |
| `OPCUA_PASSWORD` | - | Password for username authentication |
| `OPCUA_CERT_FILE` | - | Client certificate file path |
| `OPCUA_KEY_FILE` | - | Client private key file path |
| `OPCUA_SERVER_CERT` | - | Server certificate file path |
| `OPCUA_SECURITY_POLICY` | `None` | Security policy |
| `OPCUA_SECURITY_MODE` | `None` | Security mode |
| `OPCUA_REQUEST_TIMEOUT` | `30s` | Request timeout |
| `OPCUA_SESSION_TIMEOUT` | `60s` | Session timeout |
| `OPCUA_MAX_RETRIES` | `3` | Maximum retry attempts |
| `OPCUA_RETRY_DELAY` | `1s` | Delay between retries |

### MCP Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_NAME` | `OPC-UA MCP Server` | MCP server name |
| `MCP_VERSION` | `1.0.0` | MCP server version |
| `MCP_ENABLE_TOOLS` | `true` | Enable MCP tools |
| `MCP_ENABLE_RESOURCES` | `true` | Enable MCP resources |
| `MCP_ENABLE_PROMPTS` | `false` | Enable MCP prompts |
| `MCP_HTTP_PATH` | `/mcp` | HTTP endpoint path |

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

### Build and Run

```bash
# Build image
docker build -t opcua-mcp .

# Run container
docker run -p 8080:8080 \
  -e SERVER_TRANSPORT=http \
  -e OPCUA_ENDPOINT=opc.tcp://server:4840 \
  -e OPCUA_AUTH_MODE=username \
  -e OPCUA_USERNAME=admin \
  -e OPCUA_PASSWORD=secret \
  opcua-mcp
```

### Docker Compose

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
```

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
