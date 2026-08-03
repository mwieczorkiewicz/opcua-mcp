package main

import (
	"context"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/mcp"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/opcua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry"
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

	// Open the persistent store before creating the OPC-UA client (BR-10) -
	// store-open failures should surface before any network I/O is
	// attempted. A failure here is non-fatal (NFR-1.2/BR-11): the server
	// still serves every live (non-cached, non-subscribed) tool, with
	// caching forced off and subscriptions unavailable for this process
	// lifetime, since there's no sensible in-memory-only fallback for
	// persisted subscription intent.
	searchCfg := cfg.Search
	st, err := store.Open(cfg.Store.DBPath, cfg.Store.OpenTimeout)
	if err != nil {
		log.Warn("Failed to open persistent store; continuing with caching and subscriptions disabled",
			"error", err,
			"db_path", cfg.Store.DBPath,
		)
		st = nil
		searchCfg.EnableCache = false
	}

	// Create OPC-UA client
	opcuaClient := opcua.NewClient(&cfg.OPCUA)

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

	cachingClient := opcua.NewCachingClient(opcuaClient, st, &searchCfg, cfg.Store.TypeInfoTTL, cfg.Store.BrowseTTL)

	// Construct and warm-start the subscription manager only when the store
	// opened successfully. Start() re-establishes persisted subscriptions
	// (FR-2.5) before returning; a failure here is also non-fatal - the
	// server falls back to subscriptions-unavailable rather than refusing
	// to start (NFR-1.2).
	var subscriptionManager *opcua.SubscriptionManager
	if st != nil {
		mgr := opcua.NewSubscriptionManager(opcuaClient, st, &cfg.Store)
		if err := mgr.Start(context.Background()); err != nil {
			log.Warn("Failed to start subscription manager; continuing with subscriptions unavailable", "error", err)
		} else {
			subscriptionManager = mgr
		}
	}

	// Create MCP server
	mcpServer, err := mcp.NewServer(cfg, opcuaClient, cachingClient, subscriptionManager, st)
	if err != nil {
		log.Fatal("Failed to create MCP server", "error", err)
	}

	// Telemetry (see internal/telemetry/telemetry.go for what is/isn't
	// collected). Static session dimensions (transport/auth mode/security
	// policy/version) come from the already-loaded cfg rather than being
	// re-parsed from the environment a second time. Opt out via
	// DO_NOT_TRACK=1 or OPCUA_MCP_TELEMETRY=false.
	telemetryCfg := telemetry.LoadConfig()
	telemetryCfg.Transport = cfg.Server.Transport
	telemetryCfg.AuthMode = cfg.OPCUA.AuthMode
	telemetryCfg.SecurityPolicy = cfg.OPCUA.SecurityPolicy
	telemetryCfg.ServerVersion = cfg.MCP.Version
	mcpServer.SetTelemetry(telemetry.New(telemetryCfg))

	// Start MCP server
	if err := mcpServer.Start(); err != nil {
		log.Fatal("Failed to start MCP server", "error", err)
	}
}
