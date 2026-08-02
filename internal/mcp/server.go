package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/opcua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

// Server wraps the MCP server with OPC-UA functionality
type Server struct {
	mcpServer           *server.MCPServer
	opcuaClient         *opcua.Client
	cachingClient       *opcua.CachingClient
	subscriptionManager *opcua.SubscriptionManager // nil if the persistent store failed to open at startup (Unit 3 BR-11) - handlers must nil-check
	discovery           *opcua.DiscoveryService
	config              *config.Config
	store               *store.Store // nil if store.Open failed (BR-11); Close()'d strictly last during shutdown (BR-10)

	// httpServerMu guards httpServer, which is set by startHTTP() and read by
	// setupGracefulShutdown()'s signal-handling goroutine; it stays nil in
	// stdio mode.
	httpServerMu sync.Mutex
	httpServer   *server.StreamableHTTPServer
}

// NewServer creates a new MCP server with OPC-UA capabilities. cachingClient
// and subscriptionManager are constructed by the caller (cmd/opcua-mcp.go,
// per services.md's startup orchestration) since both depend on the
// persistent store, which NewServer itself has no reason to open. st is the
// same store handle, held only for Close() during graceful shutdown (BR-10)
// - Server has no other store-CRUD surface. subscriptionManager and st are
// both nil when the store failed to open at startup (BR-11); cachingClient
// is never nil (the caller constructs it with EnableCache forced false in
// that case instead).
func NewServer(cfg *config.Config, opcuaClient *opcua.Client, cachingClient *opcua.CachingClient, subscriptionManager *opcua.SubscriptionManager, st *store.Store) (*Server, error) {
	// Create MCP server with capabilities
	s := server.NewMCPServer(
		cfg.MCP.Name,
		cfg.MCP.Version,
		server.WithToolCapabilities(cfg.MCP.EnableTools),
		server.WithResourceCapabilities(cfg.MCP.EnableResources, true),
		server.WithPromptCapabilities(cfg.MCP.EnablePrompts),
		server.WithLogging(),
		server.WithRecovery(),
	)

	// Create discovery service
	discovery, err := opcua.NewDiscoveryService(opcuaClient, &cfg.Search)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery service: %w", err)
	}

	return &Server{
		mcpServer:           s,
		opcuaClient:         opcuaClient,
		cachingClient:       cachingClient,
		subscriptionManager: subscriptionManager,
		discovery:           discovery,
		config:              cfg,
		store:               st,
	}, nil
}

// SetupTools registers all OPC-UA tools with the MCP server
func (s *Server) SetupTools() error {
	// Read tool
	readTool := mcp.NewTool("opcua_read",
		mcp.WithDescription("Read values from OPC-UA nodes now. Subscribed nodes (see opcua_subscribe) are always served from the live push-based cache; others go live unless max_age_ms allows a cached value."),
		mcp.WithString("node_ids",
			mcp.Required(),
			mcp.Description("Node IDs to read. Supports multiple formats: single node (i=85), comma-separated (i=85,i=86), or JSON array ([\"i=85\",\"i=86\"])"),
		),
		mcp.WithNumber("max_age_ms",
			mcp.Description("Maximum acceptable age (milliseconds) of a cached value before falling back to a live read. 0 (default) means always live for non-subscribed nodes; subscribed nodes are always served fresh regardless of this value."),
			mcp.DefaultNumber(0),
		),
	)
	s.mcpServer.AddTool(readTool, s.handleRead)

	// Write tool
	writeTool := mcp.NewTool("opcua_write",
		mcp.WithDescription("Write values to OPC-UA nodes with automatic type validation. The tool reads the node's data type first and validates that the provided value is compatible before attempting to write."),
		mcp.WithString("node_id",
			mcp.Required(),
			mcp.Description("Node ID to write to"),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("Value to write (JSON format). Supports: booleans (true/false), integers, floats, strings, arrays, and DateTime values"),
		),
	)
	s.mcpServer.AddTool(writeTool, s.handleWrite)

	// Browse tool
	browseTool := mcp.NewTool("opcua_browse",
		mcp.WithDescription("Browse OPC-UA node hierarchy"),
		mcp.WithString("node_id",
			mcp.Description("Node ID to browse from (defaults to root)"),
			mcp.DefaultString("i=85"), // Objects folder
		),
	)
	s.mcpServer.AddTool(browseTool, s.handleBrowse)

	// Get node info tool
	nodeInfoTool := mcp.NewTool("opcua_node_info",
		mcp.WithDescription("Get information about an OPC-UA node"),
		mcp.WithString("node_id",
			mcp.Required(),
			mcp.Description("Node ID to get information for"),
		),
	)
	s.mcpServer.AddTool(nodeInfoTool, s.handleNodeInfo)

	// Server info tool
	serverInfoTool := mcp.NewTool("opcua_server_info",
		mcp.WithDescription("Get OPC-UA server information"),
	)
	s.mcpServer.AddTool(serverInfoTool, s.handleServerInfo)

	// Connect tool
	connectTool := mcp.NewTool("opcua_connect",
		mcp.WithDescription("Connect to OPC-UA server"),
	)
	s.mcpServer.AddTool(connectTool, s.handleConnect)

	// Disconnect tool
	disconnectTool := mcp.NewTool("opcua_disconnect",
		mcp.WithDescription("Disconnect from OPC-UA server"),
	)
	s.mcpServer.AddTool(disconnectTool, s.handleDisconnect)

	// Get value by node ID tool
	getValueTool := mcp.NewTool("opcua_get_value",
		mcp.WithDescription("Get value of a variable by node ID"),
		mcp.WithString("node_id",
			mcp.Required(),
			mcp.Description("Node ID of the variable to read"),
		),
	)
	s.mcpServer.AddTool(getValueTool, s.handleGetValue)

	// Browse nodes tool
	browseNodesTool := mcp.NewTool("opcua_browse_nodes",
		mcp.WithDescription("Browse through OPC-UA nodes starting from a specific node"),
		mcp.WithString("node_id",
			mcp.Description("Node ID to start browsing from (defaults to Objects folder)"),
			mcp.DefaultString("i=85"),
		),
		mcp.WithNumber("max_depth",
			mcp.Description("Maximum depth to browse (defaults to 3)"),
			mcp.DefaultNumber(3),
			mcp.Min(1),
			mcp.Max(10),
		),
	)
	s.mcpServer.AddTool(browseNodesTool, s.handleBrowseNodes)

	// Get value by browse name tool
	getValueByNameTool := mcp.NewTool("opcua_get_value_by_name",
		mcp.WithDescription("Get value of a variable by its browse name"),
		mcp.WithString("browse_name",
			mcp.Required(),
			mcp.Description("Browse name of the variable to read"),
		),
	)
	s.mcpServer.AddTool(getValueByNameTool, s.handleGetValueByName)

	// Find similar nodes tool
	findSimilarNodesTool := mcp.NewTool("opcua_find_similar_nodes",
		mcp.WithDescription("Find nodes with similar browse names"),
		mcp.WithString("browse_name",
			mcp.Required(),
			mcp.Description("Browse name to search for similar nodes"),
		),
		mcp.WithNumber("max_results",
			mcp.Description("Maximum number of results to return (defaults to 10)"),
			mcp.DefaultNumber(10),
			mcp.Min(1),
			mcp.Max(100),
		),
	)
	s.mcpServer.AddTool(findSimilarNodesTool, s.handleFindSimilarNodes)

	// Debug search tool
	debugSearchTool := mcp.NewTool("opcua_debug_search",
		mcp.WithDescription("Debug search functionality for a specific browse name"),
		mcp.WithString("browse_name",
			mcp.Required(),
			mcp.Description("Browse name to debug search for"),
		),
	)
	s.mcpServer.AddTool(debugSearchTool, s.handleDebugSearch)

	// Discovery stats tool
	discoveryStatsTool := mcp.NewTool("opcua_discovery_stats",
		mcp.WithDescription("Get discovery service statistics"),
	)
	s.mcpServer.AddTool(discoveryStatsTool, s.handleDiscoveryStats)

	// Force discovery refresh tool
	forceDiscoveryTool := mcp.NewTool("opcua_force_discovery",
		mcp.WithDescription("Force an immediate discovery refresh"),
	)
	s.mcpServer.AddTool(forceDiscoveryTool, s.handleForceDiscovery)

	// Ensure Server nodes indexed tool
	ensureServerNodesTool := mcp.NewTool("opcua_ensure_server_nodes",
		mcp.WithDescription("Ensure critical Server nodes are properly indexed"),
	)
	s.mcpServer.AddTool(ensureServerNodesTool, s.handleEnsureServerNodes)

	// Subscribe tool
	subscribeTool := mcp.NewTool("opcua_subscribe",
		mcp.WithDescription("Subscribe to push-based live updates for one or more OPC-UA nodes. Subscriptions persist across server restarts and are automatically re-established on startup."),
		mcp.WithString("node_ids",
			mcp.Required(),
			mcp.Description("Node IDs to subscribe to. Supports the same formats as opcua_read: single node, comma-separated, or JSON array"),
		),
		mcp.WithNumber("interval_ms",
			mcp.Required(),
			mcp.Description("Publishing interval in milliseconds"),
		),
	)
	s.mcpServer.AddTool(subscribeTool, s.handleSubscribe)

	// Unsubscribe tool
	unsubscribeTool := mcp.NewTool("opcua_unsubscribe",
		mcp.WithDescription("Cancel an active subscription, either by its subscription_id or by naming one or more of its subscribed node_ids (removes the entire subscription group those nodes belong to - subscriptions cannot be partially cancelled)."),
		mcp.WithString("subscription_id",
			mcp.Description("Subscription ID to cancel"),
		),
		mcp.WithString("node_ids",
			mcp.Description("Node IDs whose subscription group(s) should be cancelled - provide this or subscription_id, not both"),
		),
	)
	s.mcpServer.AddTool(unsubscribeTool, s.handleUnsubscribe)

	// List subscriptions tool
	listSubscriptionsTool := mcp.NewTool("opcua_list_subscriptions",
		mcp.WithDescription("List all active push-based subscriptions, reflecting persisted intent even immediately after a server restart, before re-subscription completes."),
	)
	s.mcpServer.AddTool(listSubscriptionsTool, s.handleListSubscriptions)

	return nil
}

// SetupResources registers all OPC-UA resources with the MCP server
func (s *Server) SetupResources() error {
	// Node resource
	nodeResource := mcp.NewResource(
		"opcua://node/{node_id}",
		"OPC-UA Node",
		mcp.WithResourceDescription("Access OPC-UA node data. Supports single node (opcua://node/i=85) or multiple nodes (opcua://node/i=85,i=86)"),
		mcp.WithMIMEType("application/json"),
	)
	s.mcpServer.AddResource(nodeResource, s.handleNodeResource)

	// Server resource
	serverResource := mcp.NewResource(
		"opcua://server",
		"OPC-UA Server",
		mcp.WithResourceDescription("OPC-UA server information"),
		mcp.WithMIMEType("application/json"),
	)
	s.mcpServer.AddResource(serverResource, s.handleServerResource)

	return nil
}

// handleNodeResource handles node resource requests
func (s *Server) handleNodeResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Extract node ID from URI
	uri := req.Params.URI
	if uri == "" {
		return nil, fmt.Errorf("node ID not specified in URI")
	}

	// Parse the URI to extract node IDs
	// Expected format: opcua://node/{node_id} or opcua://node/{node_id1,node_id2,...}
	nodeIDPart := strings.TrimPrefix(uri, "opcua://node/")
	if nodeIDPart == uri {
		return nil, fmt.Errorf("invalid URI format, expected opcua://node/{node_id}")
	}

	// Parse node IDs using the improved parsing function
	nodeIDs, err := opcua.ParseNodeIDs(nodeIDPart)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node IDs from URI: %w", err)
	}

	// Read node values
	results, err := s.opcuaClient.Read(ctx, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to read nodes: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

	// Format response - if multiple nodes, return array; if single node, return object
	var response interface{}
	if len(results) == 1 {
		// Single node response
		result := results[0]
		response = map[string]interface{}{
			"node_id":          nodeIDs[0],
			"value":            result.Value.Value(),
			"status":           result.Status,
			"source_timestamp": result.SourceTimestamp,
			"server_timestamp": result.ServerTimestamp,
		}
	} else {
		// Multiple nodes response
		var nodeResults []map[string]interface{}
		for i, result := range results {
			nodeResults = append(nodeResults, map[string]interface{}{
				"node_id":          nodeIDs[i],
				"value":            result.Value.Value(),
				"status":           result.Status,
				"source_timestamp": result.SourceTimestamp,
				"server_timestamp": result.ServerTimestamp,
			})
		}
		response = map[string]interface{}{
			"nodes": nodeResults,
			"count": len(nodeResults),
		}
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return []mcp.ResourceContents{
		&mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(responseJSON),
		},
	}, nil
}

// handleServerResource handles server resource requests
func (s *Server) handleServerResource(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Get server information
	serverStatus, err := s.opcuaClient.GetServerInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}

	// Format response
	response := map[string]interface{}{
		"start_time":   serverStatus.StartTime,
		"current_time": serverStatus.CurrentTime,
		"state":        serverStatus.State.String(),
	}

	// Add build info if available
	if serverStatus.BuildInfo != nil {
		response["product_uri"] = serverStatus.BuildInfo.ProductURI
		response["manufacturer"] = serverStatus.BuildInfo.ManufacturerName
		response["product_name"] = serverStatus.BuildInfo.ProductName
		response["software_version"] = serverStatus.BuildInfo.SoftwareVersion
		response["build_number"] = serverStatus.BuildInfo.BuildNumber
		response["build_date"] = serverStatus.BuildInfo.BuildDate
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return []mcp.ResourceContents{
		&mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "application/json",
			Text:     string(responseJSON),
		},
	}, nil
}

// handleRead handles the opcua_read tool. Since Unit 3, this delegates to
// CachingClient rather than opcuaClient directly, gaining read-through
// caching/subscription-awareness (requirements.md FR-3.1-3.3).
func (s *Server) handleRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeIDsStr := req.GetString("node_ids", "")
	if nodeIDsStr == "" {
		return mcp.NewToolResultError("node_ids parameter is required"), nil
	}

	// Parse node IDs using the improved parsing function
	nodeIDs, err := opcua.ParseNodeIDs(nodeIDsStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse node IDs: %v", err)), nil
	}

	maxAgeMs := int(req.GetFloat("max_age_ms", 0))

	// Read values via the caching layer. Since P1-1, this still returns one
	// result per requested node even when some have a bad per-node status (no
	// top-level error for that case) - each node's own "status" field below
	// already surfaces that.
	results, err := s.cachingClient.Read(ctx, nodeIDs, maxAgeMs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read from OPC-UA server: %v", err)), nil
	}

	var response []map[string]interface{}
	for _, result := range results {
		entry := map[string]interface{}{
			"node_id":          result.NodeID,
			"value":            result.Value,
			"status":           result.Status,
			"source_timestamp": result.SourceTimestamp,
			"server_timestamp": result.ServerTimestamp,
			"source":           result.Source,
		}
		if !result.CachedAt.IsZero() {
			entry["cached_at"] = result.CachedAt
		}
		response = append(response, entry)
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleWrite handles the opcua_write tool with enhanced type validation
func (s *Server) handleWrite(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID := req.GetString("node_id", "")
	if nodeID == "" {
		return mcp.NewToolResultError("node_id parameter is required"), nil
	}

	valueStr := req.GetString("value", "")
	if valueStr == "" {
		return mcp.NewToolResultError("value parameter is required"), nil
	}

	// Parse value from JSON
	var value interface{}
	if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Invalid JSON value: %v", err)), nil
	}

	// Get node type information for better error reporting. Delegates to
	// CachingClient since Unit 3 (requirements.md FR-3.5/BR-6) - a node
	// written to repeatedly benefits from typeinfo caching automatically.
	typeInfo, err := s.cachingClient.GetNodeTypeInfo(ctx, nodeID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get node type information: %v", err)), nil
	}

	// Write value to OPC-UA server (includes type validation). Delegates to
	// CachingClient since Unit 3 - invalidates any non-subscribed cached
	// value for this node on success (requirements.md FR-3.6).
	if err := s.cachingClient.Write(ctx, nodeID, value); err != nil {
		// Provide detailed error information including type details
		errorMsg := fmt.Sprintf("Failed to write to OPC-UA server: %v", err)

		// Add type information to help user understand what went wrong
		if typeInfo != nil {
			errorMsg += "\n\nNode type information:"
			errorMsg += fmt.Sprintf("\n- Data Type ID: %d", typeInfo.DataType.IntID())
			errorMsg += fmt.Sprintf("\n- Value Rank: %d", typeInfo.ValueRank)
			errorMsg += fmt.Sprintf("\n- Is Array: %t", typeInfo.IsArray)
			errorMsg += fmt.Sprintf("\n- Is Scalar: %t", typeInfo.IsScalar)
			errorMsg += fmt.Sprintf("\n- Access Level: %d", typeInfo.AccessLevel)
			errorMsg += fmt.Sprintf("\n- User Access Level: %d", typeInfo.UserAccessLevel)
			errorMsg += fmt.Sprintf("\n- Is Writable: %t", typeInfo.IsWritable)
			errorMsg += fmt.Sprintf("\n- Is User Writable: %t", typeInfo.IsUserWritable)

			if len(typeInfo.ArrayDimensions) > 0 {
				errorMsg += fmt.Sprintf("\n- Array Dimensions: %v", typeInfo.ArrayDimensions)
			}

			// Add helpful hints based on the data type
			switch typeInfo.DataType.IntID() {
			case uint32(ua.TypeIDBoolean):
				errorMsg += "\n\nHint: This node expects a boolean value (true/false)"
			case uint32(ua.TypeIDSByte), uint32(ua.TypeIDByte), uint32(ua.TypeIDInt16), uint32(ua.TypeIDUint16), uint32(ua.TypeIDInt32), uint32(ua.TypeIDUint32), uint32(ua.TypeIDInt64), uint32(ua.TypeIDUint64):
				errorMsg += "\n\nHint: This node expects an integer value"
			case uint32(ua.TypeIDFloat), uint32(ua.TypeIDDouble):
				errorMsg += "\n\nHint: This node expects a floating-point number"
			case uint32(ua.TypeIDString):
				errorMsg += "\n\nHint: This node expects a string value"
			case uint32(ua.TypeIDDateTime):
				errorMsg += "\n\nHint: This node expects a DateTime value (string or number)"
			}

			// Add writability hint
			if !typeInfo.IsUserWritable {
				errorMsg += "\n\nNote: This node is not writable for the current user"
			}
		}

		return mcp.NewToolResultError(errorMsg), nil
	}

	// Success response with type information
	successMsg := fmt.Sprintf("Successfully wrote value to node %s", nodeID)
	if typeInfo != nil {
		successMsg += "\n\nType information:"
		successMsg += fmt.Sprintf("\n- Data Type ID: %d", typeInfo.DataType.IntID())
		successMsg += fmt.Sprintf("\n- Value Rank: %d", typeInfo.ValueRank)
		successMsg += fmt.Sprintf("\n- Is Array: %t", typeInfo.IsArray)
		successMsg += fmt.Sprintf("\n- Is Scalar: %t", typeInfo.IsScalar)
		successMsg += fmt.Sprintf("\n- Access Level: %d", typeInfo.AccessLevel)
		successMsg += fmt.Sprintf("\n- User Access Level: %d", typeInfo.UserAccessLevel)
		successMsg += fmt.Sprintf("\n- Is Writable: %t", typeInfo.IsWritable)
		successMsg += fmt.Sprintf("\n- Is User Writable: %t", typeInfo.IsUserWritable)
	}

	return mcp.NewToolResultText(successMsg), nil
}

// handleBrowse handles the opcua_browse tool
func (s *Server) handleBrowse(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID := req.GetString("node_id", "i=85") // Default to Objects folder

	// Browse OPC-UA server
	references, err := s.opcuaClient.Browse(ctx, nodeID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to browse OPC-UA server: %v", err)), nil
	}

	// Format results
	var response []map[string]interface{}
	for _, ref := range references {
		response = append(response, map[string]interface{}{
			"node_id":         ref.NodeID.String(),
			"browse_name":     ref.BrowseName.Name,
			"display_name":    ref.DisplayName.Text,
			"node_class":      ref.NodeClass.String(),
			"type_definition": ref.TypeDefinition.String(),
		})
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleNodeInfo handles the opcua_node_info tool
func (s *Server) handleNodeInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID := req.GetString("node_id", "")
	if nodeID == "" {
		return mcp.NewToolResultError("node_id parameter is required"), nil
	}

	// Get node class
	nodeClass, err := s.opcuaClient.GetNodeClass(ctx, nodeID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get node class: %v", err)), nil
	}

	// Get node value if it's a variable
	var value interface{}
	if nodeClass == ua.NodeClassVariable {
		results, err := s.opcuaClient.Read(ctx, []string{nodeID})
		if err == nil && len(results) > 0 {
			value = results[0].Value.Value()
		}
	}

	// Format response
	response := map[string]interface{}{
		"node_id":    nodeID,
		"node_class": nodeClass.String(),
		"value":      value,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

const serverStatusNode = "i=2256"

// handleServerInfo handles the opcua_server_info tool
func (s *Server) handleServerInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	serverStatus, err := s.opcuaClient.GetServerInfo(ctx)
	if err != nil {
		return s.handleServerInfoFallback(ctx, err)
	}

	response := s.formatServerStatusResponse(serverStatus, "get_server_info")
	return s.marshalResponse(response)
}

func (s *Server) handleServerInfoFallback(ctx context.Context, primaryErr error) (*mcp.CallToolResult, error) {
	results, err := s.opcuaClient.Read(ctx, []string{serverStatusNode})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get server info: %v (fallback also failed: %v)", primaryErr, err)), nil
	}

	if len(results) == 0 {
		return mcp.NewToolResultError("No server status data returned from fallback"), nil
	}

	result := results[0]
	if result.Status != ua.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Server status read failed: %s", result.Status)), nil
	}

	response := map[string]interface{}{
		"node_id":          serverStatusNode,
		"server_status":    result.Value.Value(),
		"source_timestamp": result.SourceTimestamp,
		"server_timestamp": result.ServerTimestamp,
		"status":           result.Status,
		"method":           "fallback_read",
	}

	return s.marshalResponse(response)
}

func (s *Server) formatServerStatusResponse(serverStatus *ua.ServerStatusDataType, method string) map[string]interface{} {
	response := map[string]interface{}{
		"start_time":   serverStatus.StartTime,
		"current_time": serverStatus.CurrentTime,
		"state":        serverStatus.State.String(),
		"method":       method,
	}

	if serverStatus.BuildInfo != nil {
		buildInfo := serverStatus.BuildInfo
		response["product_uri"] = buildInfo.ProductURI
		response["manufacturer"] = buildInfo.ManufacturerName
		response["product_name"] = buildInfo.ProductName
		response["software_version"] = buildInfo.SoftwareVersion
		response["build_number"] = buildInfo.BuildNumber
		response["build_date"] = buildInfo.BuildDate
	}

	return response
}

func (s *Server) marshalResponse(response map[string]interface{}) (*mcp.CallToolResult, error) {
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleConnect handles the opcua_connect tool
func (s *Server) handleConnect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.opcuaClient.IsConnected() {
		return mcp.NewToolResultText("Already connected to OPC-UA server"), nil
	}

	if err := s.opcuaClient.Connect(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to connect to OPC-UA server: %v", err)), nil
	}

	return mcp.NewToolResultText("Successfully connected to OPC-UA server"), nil
}

// handleDisconnect handles the opcua_disconnect tool
func (s *Server) handleDisconnect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !s.opcuaClient.IsConnected() {
		return mcp.NewToolResultText("Not connected to OPC-UA server"), nil
	}

	if err := s.opcuaClient.Disconnect(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to disconnect from OPC-UA server: %v", err)), nil
	}

	return mcp.NewToolResultText("Successfully disconnected from OPC-UA server"), nil
}

// Start starts the MCP server based on the configured transport
func (s *Server) Start() error {
	// Setup tools and resources
	if err := s.SetupTools(); err != nil {
		return fmt.Errorf("failed to setup tools: %w", err)
	}

	if err := s.SetupResources(); err != nil {
		return fmt.Errorf("failed to setup resources: %w", err)
	}

	// Setup graceful shutdown
	s.setupGracefulShutdown()

	// Start discovery service
	ctx := context.Background()
	if err := s.discovery.Start(ctx); err != nil {
		logger.Warn("Failed to start discovery service", "error", err)
	}

	// Start server based on transport mode
	switch s.config.Server.Transport {
	case "stdio":
		return s.startStdio()
	case "http":
		return s.startHTTP()
	default:
		return fmt.Errorf("unsupported transport mode: %s", s.config.Server.Transport)
	}
}

// startStdio starts the server in stdio mode
func (s *Server) startStdio() error {
	logger.Info("Starting MCP server in stdio mode")
	return server.ServeStdio(s.mcpServer)
}

// startHTTP starts the server in HTTP mode
func (s *Server) startHTTP() error {
	logger.Info("Starting MCP server in HTTP mode", "port", s.config.Server.HTTPPort)

	// Create HTTP server
	httpServer := server.NewStreamableHTTPServer(s.mcpServer)

	// Store the handle so setupGracefulShutdown can drain it on signal.
	s.httpServerMu.Lock()
	s.httpServer = httpServer
	s.httpServerMu.Unlock()

	// Start server
	return httpServer.Start(":" + s.config.Server.HTTPPort)
}

// handleGetValue handles the opcua_get_value tool
func (s *Server) handleGetValue(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID := req.GetString("node_id", "")
	if nodeID == "" {
		return mcp.NewToolResultError("node_id parameter is required"), nil
	}

	// Read value from OPC-UA server
	results, err := s.opcuaClient.Read(ctx, []string{nodeID})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read from OPC-UA server: %v", err)), nil
	}

	if len(results) == 0 {
		return mcp.NewToolResultError("No results returned"), nil
	}

	// Per P1-1, a bad per-node status surfaces here rather than as a
	// top-level error - the "status" field below reflects it directly.
	result := results[0]
	response := map[string]interface{}{
		"node_id":          nodeID,
		"value":            result.Value.Value(),
		"status":           result.Status,
		"source_timestamp": result.SourceTimestamp,
		"server_timestamp": result.ServerTimestamp,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleBrowseNodes handles the opcua_browse_nodes tool
func (s *Server) handleBrowseNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID := req.GetString("node_id", "i=85")
	maxDepth := int(req.GetFloat("max_depth", 3))

	// Browse OPC-UA server with depth limit
	references, err := s.browseNodesWithDepth(ctx, nodeID, maxDepth, 0)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to browse OPC-UA server: %v", err)), nil
	}

	responseJSON, err := json.MarshalIndent(references, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleGetValueByName handles the opcua_get_value_by_name tool
func (s *Server) handleGetValueByName(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	browseName := req.GetString("browse_name", "")
	if browseName == "" {
		return mcp.NewToolResultError("browse_name parameter is required"), nil
	}

	// Find node by browse name
	nodeInfo, err := s.discovery.GetNodeByBrowseName(browseName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to find node with browse name '%s': %v", browseName, err)), nil
	}

	// Read value from the found node
	results, err := s.opcuaClient.Read(ctx, []string{nodeInfo.NodeID})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read from OPC-UA server: %v", err)), nil
	}

	if len(results) == 0 {
		return mcp.NewToolResultError("No results returned"), nil
	}

	result := results[0]
	response := map[string]interface{}{
		"node_id":          nodeInfo.NodeID,
		"browse_name":      nodeInfo.BrowseName,
		"display_name":     nodeInfo.DisplayName,
		"node_class":       nodeInfo.NodeClass,
		"value":            result.Value.Value(),
		"status":           result.Status,
		"source_timestamp": result.SourceTimestamp,
		"server_timestamp": result.ServerTimestamp,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleFindSimilarNodes handles the opcua_find_similar_nodes tool
func (s *Server) handleFindSimilarNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	browseName := req.GetString("browse_name", "")
	if browseName == "" {
		return mcp.NewToolResultError("browse_name parameter is required"), nil
	}

	maxResults := int(req.GetFloat("max_results", 10))

	// Find similar nodes
	similarNodes, err := s.discovery.FindSimilarNodes(browseName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to find similar nodes: %v", err)), nil
	}

	// Limit results
	if len(similarNodes) > maxResults {
		similarNodes = similarNodes[:maxResults]
	}

	// Format response
	var response []map[string]interface{}
	for _, node := range similarNodes {
		response = append(response, map[string]interface{}{
			"node_id":         node.NodeID,
			"browse_name":     node.BrowseName,
			"display_name":    node.DisplayName,
			"node_class":      node.NodeClass,
			"type_definition": node.TypeDefinition,
			"parent_node":     node.ParentNode,
			"depth":           node.Depth,
		})
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleDebugSearch handles the opcua_debug_search tool
func (s *Server) handleDebugSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	browseName := req.GetString("browse_name", "")
	if browseName == "" {
		return mcp.NewToolResultError("browse_name parameter is required"), nil
	}

	// Get debug information
	debugInfo, err := s.discovery.DebugSearchNodes(browseName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get debug information: %v", err)), nil
	}

	responseJSON, err := json.MarshalIndent(debugInfo, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleDiscoveryStats handles the opcua_discovery_stats tool
func (s *Server) handleDiscoveryStats(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stats := s.discovery.GetCacheStats()

	responseJSON, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleForceDiscovery handles the opcua_force_discovery tool
func (s *Server) handleForceDiscovery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Force discovery refresh
	if err := s.discovery.ForceDiscoveryRefresh(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to force discovery refresh: %v", err)), nil
	}

	// Get updated stats
	stats := s.discovery.GetCacheStats()

	response := map[string]interface{}{
		"message": "Discovery refresh completed successfully",
		"stats":   stats,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleEnsureServerNodes handles the opcua_ensure_server_nodes tool
func (s *Server) handleEnsureServerNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Ensure Server nodes are properly indexed
	if err := s.discovery.EnsureServerNodesIndexed(ctx); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to ensure Server nodes are indexed: %v", err)), nil
	}

	// Get updated stats
	stats := s.discovery.GetCacheStats()

	response := map[string]interface{}{
		"message": "Server nodes indexing verified successfully",
		"stats":   stats,
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// subscriptionsUnavailableResult is returned by all 3 subscription tool
// handlers when subscriptionManager is nil - the persistent store failed to
// open at startup (requirements.md NFR-1.2, business-rules.md BR-11) and
// there is no sensible in-memory-only fallback for subscription intent.
func subscriptionsUnavailableResult() *mcp.CallToolResult {
	return mcp.NewToolResultError("Subscriptions are unavailable: the persistent store failed to open at startup")
}

// handleSubscribe handles the opcua_subscribe tool
func (s *Server) handleSubscribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.subscriptionManager == nil {
		return subscriptionsUnavailableResult(), nil
	}

	nodeIDsStr := req.GetString("node_ids", "")
	if nodeIDsStr == "" {
		return mcp.NewToolResultError("node_ids parameter is required"), nil
	}
	nodeIDs, err := opcua.ParseNodeIDs(nodeIDsStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to parse node IDs: %v", err)), nil
	}

	intervalMs := int(req.GetFloat("interval_ms", 0))
	if intervalMs <= 0 {
		return mcp.NewToolResultError("interval_ms parameter is required and must be positive"), nil
	}

	subscriptionID, rejected, err := s.subscriptionManager.Subscribe(ctx, nodeIDs, intervalMs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to subscribe: %v", err)), nil
	}

	rejectedSet := make(map[string]bool, len(rejected))
	for _, r := range rejected {
		rejectedSet[r.NodeID] = true
	}
	accepted := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if !rejectedSet[nodeID] {
			accepted = append(accepted, nodeID)
		}
	}

	return s.marshalResponse(map[string]interface{}{
		"subscription_id": subscriptionID,
		"accepted":        accepted,
		"rejected":        rejected,
		"interval_ms":     intervalMs,
	})
}

// handleUnsubscribe handles the opcua_unsubscribe tool. Accepts either
// subscription_id (removes that group directly) or node_ids (removes every
// subscription group any of those nodes belongs to, in whole -
// business-rules.md BR-8).
func (s *Server) handleUnsubscribe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.subscriptionManager == nil {
		return subscriptionsUnavailableResult(), nil
	}

	subscriptionID := req.GetString("subscription_id", "")
	nodeIDsStr := req.GetString("node_ids", "")
	if subscriptionID == "" && nodeIDsStr == "" {
		return mcp.NewToolResultError("either subscription_id or node_ids is required"), nil
	}

	var toRemove []string
	if subscriptionID != "" {
		toRemove = []string{subscriptionID}
	} else {
		nodeIDs, err := opcua.ParseNodeIDs(nodeIDsStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to parse node IDs: %v", err)), nil
		}
		requested := make(map[string]bool, len(nodeIDs))
		for _, nodeID := range nodeIDs {
			requested[nodeID] = true
		}
		for _, info := range s.subscriptionManager.ListSubscriptions() {
			for _, nodeID := range info.NodeIDs {
				if requested[nodeID] {
					toRemove = append(toRemove, info.ID)
					break
				}
			}
		}
	}

	unsubscribed := make([]string, 0, len(toRemove))
	for _, id := range toRemove {
		if err := s.subscriptionManager.Unsubscribe(ctx, id); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to unsubscribe %s: %v", id, err)), nil
		}
		unsubscribed = append(unsubscribed, id)
	}

	return s.marshalResponse(map[string]interface{}{"unsubscribed": unsubscribed})
}

// handleListSubscriptions handles the opcua_list_subscriptions tool
func (s *Server) handleListSubscriptions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.subscriptionManager == nil {
		return subscriptionsUnavailableResult(), nil
	}

	infos := s.subscriptionManager.ListSubscriptions()
	response := make([]map[string]interface{}, 0, len(infos))
	for _, info := range infos {
		response = append(response, map[string]interface{}{
			"subscription_id": info.ID,
			"node_ids":        info.NodeIDs,
			"interval_ms":     info.IntervalMs,
			"created_at":      info.CreatedAt,
		})
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(responseJSON)), nil
}

// browseNodesWithDepth recursively browses nodes with depth limit. Delegates
// to CachingClient since Unit 3 (requirements.md FR-3.4) - each level's
// browse result is cached, keyed by its own parent node ID.
func (s *Server) browseNodesWithDepth(ctx context.Context, nodeID string, maxDepth, currentDepth int) ([]map[string]interface{}, error) {
	if currentDepth >= maxDepth {
		return nil, nil
	}

	// Browse the current node
	references, err := s.cachingClient.Browse(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, ref := range references {
		nodeInfo := map[string]interface{}{
			"node_id":         ref.NodeID,
			"browse_name":     ref.BrowseName,
			"display_name":    ref.DisplayName,
			"node_class":      ref.NodeClass,
			"type_definition": ref.TypeDefinition,
			"depth":           currentDepth,
		}

		// Recursively browse child nodes if not at max depth
		if currentDepth < maxDepth-1 {
			children, err := s.browseNodesWithDepth(ctx, ref.NodeID, maxDepth, currentDepth+1)
			if err == nil && len(children) > 0 {
				nodeInfo["children"] = children
			}
		}

		result = append(result, nodeInfo)
	}

	return result, nil
}

// setupGracefulShutdown sets up graceful shutdown handling
func (s *Server) setupGracefulShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		logger.Info("Received shutdown signal")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Stop discovery service
		if err := s.discovery.Stop(); err != nil {
			logger.Error("Error stopping discovery service", "error", err)
		}

		// Stop the subscription manager (joins its notification pump and its
		// internal ReconnectWatcher) before the store closes (BR-10) - a
		// write-after-close from an in-flight batch must never happen. Nil
		// when the persistent store failed to open at startup (BR-11).
		if s.subscriptionManager != nil {
			if err := s.subscriptionManager.Stop(); err != nil {
				logger.Error("Error stopping subscription manager", "error", err)
			}
		}

		// Close the HTTP server so in-flight requests get a chance to drain
		// before process exit (findings.md H10c). No-op in stdio mode, where
		// httpServer stays nil.
		s.httpServerMu.Lock()
		httpServer := s.httpServer
		s.httpServerMu.Unlock()
		if httpServer != nil {
			if err := httpServer.Shutdown(ctx); err != nil {
				logger.Error("Error shutting down HTTP server", "error", err)
			}
		}

		// Disconnect from OPC-UA server
		if err := s.opcuaClient.Disconnect(ctx); err != nil {
			logger.Error("Error disconnecting from OPC-UA server", "error", err)
		}

		// store.Close() is strictly last (BR-10) - the subscription
		// manager's pump goroutine has already been joined above, so no
		// write-after-close race is possible. Nil when store.Open failed at
		// startup (BR-11).
		if s.store != nil {
			if err := s.store.Close(); err != nil {
				logger.Error("Error closing persistent store", "error", err)
			}
		}

		os.Exit(0)
	}()
}
