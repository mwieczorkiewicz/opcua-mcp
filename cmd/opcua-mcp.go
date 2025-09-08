package main

import (
	"context"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/mcp"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/opcua"
)

func main() {
	// Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		// Use basic logging for config errors
		panic("Failed to load configuration: " + err.Error())
	}

	// Initialize logger
	if err := logger.Init(&cfg.Server); err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}

	log := logger.Get()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatal("Configuration validation failed", "error", err)
	}

	log.Info("Starting OPC-UA MCP Server",
		"name", cfg.MCP.Name,
		"version", cfg.MCP.Version,
		"transport", cfg.Server.Transport,
		"endpoint", cfg.OPCUA.Endpoint,
	)

	// Create OPC-UA client
	opcuaClient := opcua.NewClient(&cfg.OPCUA)

	// Create MCP server
	mcpServer, err := mcp.NewServer(cfg, opcuaClient)
	if err != nil {
		log.Fatal("Failed to create MCP server", "error", err)
	}

	// Connect to OPC-UA server if not in stdio mode
	if cfg.Server.Transport != "stdio" {
		ctx := context.Background()
		if err := opcuaClient.Connect(ctx); err != nil {
			log.Warn("Failed to connect to OPC-UA server",
				"error", err,
				"endpoint", cfg.OPCUA.Endpoint,
			)
			log.Info("Server will start but OPC-UA operations will fail until connection is established")
		} else {
			log.Info("Successfully connected to OPC-UA server",
				"endpoint", cfg.OPCUA.Endpoint,
			)
		}
	}

	// Start MCP server
	if err := mcpServer.Start(); err != nil {
		log.Fatal("Failed to start MCP server", "error", err)
	}
}
