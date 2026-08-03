# API Documentation

This server exposes no REST API - its only external-facing surface is the
Model Context Protocol (MCP), over stdio or one streamable-HTTP endpoint
(`MCP_HTTP_PATH`, default `/mcp`). All 15 tools below are registered in
`internal/mcp/server.go`'s `SetupTools()`.

## MCP Tools

### `opcua_read`
- **Handler**: `handleRead` → `Client.Read`
- **Purpose**: Read one or more node values.
- **Request**: `node_ids` (required, string - single/CSV/JSON-array node IDs)
- **Response**: JSON array of `{node_id, value, status, source_timestamp, server_timestamp}`, one per requested node (partial results preserved even if some nodes have a bad status).

### `opcua_write`
- **Handler**: `handleWrite` → `Client.GetNodeTypeInfo` + `Client.Write`
- **Purpose**: Write a type-validated, type-converted value to a node.
- **Request**: `node_id` (required, string), `value` (required, string - JSON-encoded)
- **Response**: Success/error text including the node's type information (data type, access level, writability) to help diagnose a rejected write.

### `opcua_browse`
- **Handler**: `handleBrowse` → `Client.Browse`
- **Purpose**: Browse one level of a node's children.
- **Request**: `node_id` (optional, default `i=85` - Objects folder)
- **Response**: JSON array of `{node_id, browse_name, display_name, node_class, type_definition}`.

### `opcua_node_info`
- **Handler**: `handleNodeInfo` → `Client.GetNodeClass` (+ `Client.Read` if variable)
- **Purpose**: Get a node's class and, if it's a variable, its current value.
- **Request**: `node_id` (required, string)
- **Response**: `{node_id, node_class, value}`.

### `opcua_server_info`
- **Handler**: `handleServerInfo` → `Client.GetServerInfo` (falls back to a raw read of the ServerStatus node)
- **Purpose**: OPC-UA server status (start/current time, state, build info).
- **Request**: none

### `opcua_connect`
- **Handler**: `handleConnect` → `Client.Connect`
- **Purpose**: Explicitly connect (primarily relevant in stdio mode, which connects lazily).
- **Request**: none

### `opcua_disconnect`
- **Handler**: `handleDisconnect` → `Client.Disconnect`
- **Purpose**: Explicitly disconnect.
- **Request**: none

### `opcua_get_value`
- **Handler**: `handleGetValue` → `Client.Read` (single node)
- **Purpose**: Convenience single-node read. **Known overlap**: strict
  functional subset of `opcua_read` (see `code-quality-assessment.md`).
- **Request**: `node_id` (required, string)

### `opcua_browse_nodes`
- **Handler**: `handleBrowseNodes` → `browseNodesWithDepth` (recursive, calls `Client.Browse` per level)
- **Purpose**: Recursive browse with a depth limit, results nested by level.
- **Request**: `node_id` (optional, default `i=85`), `max_depth` (optional, default `3`, range 1-10)

### `opcua_get_value_by_name`
- **Handler**: `handleGetValueByName` → `DiscoveryService.GetNodeByBrowseName` + `Client.Read`
- **Purpose**: Read a value by browse name instead of node ID, via the discovery cache.
- **Request**: `browse_name` (required, string)

### `opcua_find_similar_nodes`
- **Handler**: `handleFindSimilarNodes` → `DiscoveryService.FindSimilarNodes`
- **Purpose**: Tiered (exact/wildcard/fuzzy) browse-name search.
- **Request**: `browse_name` (required, string), `max_results` (optional, default `10`, range 1-100)

### `opcua_discovery_stats`
- **Handler**: `handleDiscoveryStats` → `DiscoveryService.GetCacheStats`
- **Purpose**: Cache size, depth distribution, feature-flag state.
- **Request**: none

### `opcua_force_discovery`
- **Handler**: `handleForceDiscovery` → `DiscoveryService.ForceDiscoveryRefresh`
- **Purpose**: Trigger an immediate discovery walk instead of waiting for the ticker.
- **Request**: none

### `opcua_debug_search` (diagnostic)
- **Handler**: `handleDebugSearch` → `DiscoveryService.DebugSearchNodes`
- **Purpose**: Reports cache/search-index stats and runs multiple search strategies against a browse name, to help debug why a node isn't found.
- **Request**: `browse_name` (required, string)

### `opcua_ensure_server_nodes` (diagnostic)
- **Handler**: `handleEnsureServerNodes` → `DiscoveryService.EnsureServerNodesIndexed`
- **Purpose**: Verifies the standard `Server`/`ServerStatus` nodes are indexed, forcing a refresh if not.
- **Request**: none

## MCP Resources

### `opcua://node/{node_id}`
- **Handler**: `handleNodeResource` → `Client.Read`
- **Purpose**: Same data as `opcua_read`, exposed as a readable MCP resource URI instead of a tool call. Supports single or comma-separated node IDs in the URI.

### `opcua://server`
- **Handler**: `handleServerResource` → `Client.GetServerInfo`
- **Purpose**: Same data as `opcua_server_info`, as a resource.

## Internal APIs (selected, most consequential)

### `opcua.Client` (`internal/opcua/client.go`)
- `Connect(ctx) error`, `Disconnect(ctx) error`, `IsConnected() bool`
- `Read(ctx, nodeIDs []string) ([]*ua.DataValue, error)` - per-node partial
  results; only transport-level failures return a top-level error.
- `Write(ctx, nodeID string, value interface{}) error` - fetches
  `GetNodeTypeInfo` once, validates via `ValidateValueForNode`, converts via
  `convertValueToOPCUAType`, then writes; returns an error (not just a
  logged warning) on validation failure.
- `Browse(ctx, nodeID string) ([]*ua.ReferenceDescription, error)` - follows
  `BrowseNext` continuation points, bounded by `ctx` and a hard iteration cap.
- `GetNodeClass`/`GetNodeDataType`/`GetNodeValueRank`/`GetNodeArrayDimensions`/
  `GetNodeAccessLevel`/`GetNodeUserAccessLevel`(ctx, nodeID) - single-attribute
  reads underlying `GetNodeTypeInfo`.
- `GetNodeTypeInfo(ctx, nodeID) (*NodeTypeInfo, error)` - 5 reads, aggregated.
- `ValidateValueForNode(value, typeInfo *NodeTypeInfo) error` - takes an
  already-fetched `NodeTypeInfo` (no redundant fetch).

### `opcua.DiscoveryService` (`internal/opcua/discovery.go`)
- `Start(ctx) error`/`Stop() error` - ticker-driven background worker lifecycle.
- `GetNodeInfo(nodeID) (*NodeInfo, error)`, `GetAllNodes() []*NodeInfo`,
  `GetNodeHierarchy(nodeID) ([]*NodeInfo, error)`.
- `GetNodeByBrowseName(browseName) (*NodeInfo, error)`,
  `FindSimilarNodes(browseName) ([]*NodeInfo, error)`.
- `SearchNodes(query, nodeClass string, maxResults int) ([]*NodeInfo, error)`,
  `SearchByNodeClass`/`SearchByParent`/`SearchByDepth`.
- `GetCacheStats() map[string]interface{}`, `GetSearchStats() (map[string]interface{}, error)`.
- `ForceDiscoveryRefresh(ctx) error`, `EnsureServerNodesIndexed(ctx) error`,
  `DebugSearchNodes(browseName) (map[string]interface{}, error)`.

## Data Models

### `NodeInfo` (`internal/opcua/discovery.go`)
- **Fields**: `NodeID`, `BrowseName`, `DisplayName`, `NodeClass`,
  `TypeDefinition`, `ParentNode`, `Depth`, `LastUpdated` (all JSON-exposed),
  `SeenInGen` (internal mark-and-sweep bookkeeping, `json:"-"`, excluded from
  both the tool-facing JSON shape and the Bleve document).
- **Relationships**: `ParentNode` links to another `NodeInfo.NodeID`
  (`GetNodeHierarchy` walks this chain).

### `NodeTypeInfo` (`internal/opcua/client.go`)
- **Fields**: `DataType`, `ValueRank`, `ArrayDimensions`, `IsArray`,
  `IsScalar`, `AccessLevel`, `UserAccessLevel`, `IsWritable`, `IsUserWritable`.
- **Validation**: `ValidateValueForNode` checks writability and, per
  `DataType`, the value's Go type/range/array-shape against 15 OPC-UA
  built-in data types.
