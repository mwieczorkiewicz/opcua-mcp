package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/opcua"
)

// Server wraps the MCP server with OPC-UA functionality
type Server struct {
	mcpServer   *server.MCPServer
	opcuaClient *opcua.Client
	discovery   *opcua.DiscoveryService
	config      *config.Config
}

// NewServer creates a new MCP server with OPC-UA capabilities
func NewServer(cfg *config.Config, opcuaClient *opcua.Client) (*Server, error) {
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
		mcpServer:   s,
		opcuaClient: opcuaClient,
		discovery:   discovery,
		config:      cfg,
	}, nil
}

// SetupTools registers all OPC-UA tools with the MCP server
func (s *Server) SetupTools() error {
	// Read tool
	readTool := mcp.NewTool("opcua_read",
		mcp.WithDescription("Read values from OPC-UA nodes"),
		mcp.WithString("node_ids",
			mcp.Required(),
			mcp.Description("Comma-separated list of node IDs to read"),
		),
	)
	s.mcpServer.AddTool(readTool, s.handleRead)

	// Write tool
	writeTool := mcp.NewTool("opcua_write",
		mcp.WithDescription("Write values to OPC-UA nodes"),
		mcp.WithString("node_id",
			mcp.Required(),
			mcp.Description("Node ID to write to"),
		),
		mcp.WithString("value",
			mcp.Required(),
			mcp.Description("Value to write (JSON format)"),
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

	return nil
}

// SetupResources registers all OPC-UA resources with the MCP server
func (s *Server) SetupResources() error {
	// Node resource
	nodeResource := mcp.NewResource(
		"opcua://node/{node_id}",
		"OPC-UA Node",
		mcp.WithResourceDescription("Access OPC-UA node data"),
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
	nodeID := req.Params.URI
	if nodeID == "" {
		return nil, fmt.Errorf("node ID not specified in URI")
	}

	// Read node value
	results, err := s.opcuaClient.Read(ctx, []string{nodeID})
	if err != nil {
		return nil, fmt.Errorf("failed to read node: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

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

// handleRead handles the opcua_read tool
func (s *Server) handleRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeIDsStr := req.GetString("node_ids", "")
	if nodeIDsStr == "" {
		return mcp.NewToolResultError("node_ids parameter is required"), nil
	}

	// Parse comma-separated node IDs
	var nodeIDs []string
	if err := json.Unmarshal([]byte("["+nodeIDsStr+"]"), &nodeIDs); err != nil {
		// Try splitting by comma if JSON parsing fails
		nodeIDs = []string{nodeIDsStr}
	}

	// Read values from OPC-UA server
	results, err := s.opcuaClient.Read(ctx, nodeIDs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read from OPC-UA server: %v", err)), nil
	}

	// Format results
	var response []map[string]interface{}
	for i, result := range results {
		response = append(response, map[string]interface{}{
			"node_id":          nodeIDs[i],
			"value":            result.Value.Value(),
			"status":           result.Status,
			"source_timestamp": result.SourceTimestamp,
			"server_timestamp": result.ServerTimestamp,
		})
	}

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(responseJSON)), nil
}

// handleWrite handles the opcua_write tool
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

	// Write value to OPC-UA server
	if err := s.opcuaClient.Write(ctx, nodeID, value); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to write to OPC-UA server: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Successfully wrote value to node %s", nodeID)), nil
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

// handleServerInfo handles the opcua_server_info tool
func (s *Server) handleServerInfo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get server information
	serverStatus, err := s.opcuaClient.GetServerInfo(ctx)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get server info: %v", err)), nil
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

// browseNodesWithDepth recursively browses nodes with depth limit
func (s *Server) browseNodesWithDepth(ctx context.Context, nodeID string, maxDepth, currentDepth int) ([]map[string]interface{}, error) {
	if currentDepth >= maxDepth {
		return nil, nil
	}

	// Browse the current node
	references, err := s.opcuaClient.Browse(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, ref := range references {
		nodeInfo := map[string]interface{}{
			"node_id":         ref.NodeID.String(),
			"browse_name":     ref.BrowseName.Name,
			"display_name":    ref.DisplayName.Text,
			"node_class":      ref.NodeClass.String(),
			"type_definition": ref.TypeDefinition.String(),
			"depth":           currentDepth,
		}

		// Recursively browse child nodes if not at max depth
		if currentDepth < maxDepth-1 {
			children, err := s.browseNodesWithDepth(ctx, ref.NodeID.String(), maxDepth, currentDepth+1)
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

		// Disconnect from OPC-UA server
		if err := s.opcuaClient.Disconnect(ctx); err != nil {
			logger.Error("Error disconnecting from OPC-UA server", "error", err)
		}

		os.Exit(0)
	}()
}
