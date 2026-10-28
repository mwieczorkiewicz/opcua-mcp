package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// readOnlyTools is the read-only preset: every tool that never changes data
// on the OPC-UA server or the connection/subscription state other clients
// rely on. opcua_connect stays in because stdio mode connects only through
// it; the discovery tools stay in because they only browse. Anything not
// listed here, including any tool added later, is denied in read-only mode
// unless MCP_ALLOWED_TOOLS names it, so a new mutating tool can't slip
// through by default.
var readOnlyTools = map[string]bool{
	"opcua_read":                true,
	"opcua_browse":              true,
	"opcua_node_info":           true,
	"opcua_server_info":         true,
	"opcua_connect":             true,
	"opcua_get_value":           true,
	"opcua_browse_nodes":        true,
	"opcua_get_value_by_name":   true,
	"opcua_find_similar_nodes":  true,
	"opcua_debug_search":        true,
	"opcua_discovery_stats":     true,
	"opcua_force_discovery":     true,
	"opcua_ensure_server_nodes": true,
	"opcua_list_subscriptions":  true,
}

// toolPolicy decides which registered tools are exposed. It is immutable
// once built, so it needs no locking.
type toolPolicy struct {
	readOnly bool
	allowed  map[string]bool // extra tools approved via MCP_ALLOWED_TOOLS
}

// newToolPolicy builds the policy from MCP_READ_ONLY/MCP_ALLOWED_TOOLS.
// registered is the full set of tool names the server registered; any
// allowlist entry outside it is a startup error, so a typo can't silently
// leave a tool disabled.
func newToolPolicy(readOnly bool, allowedTools string, registered []string) (*toolPolicy, error) {
	known := make(map[string]bool, len(registered))
	for _, name := range registered {
		known[name] = true
	}

	allowed := parseAllowedTools(allowedTools)
	var unknown []string
	for name := range allowed {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		valid := append([]string(nil), registered...)
		sort.Strings(valid)
		return nil, fmt.Errorf("MCP_ALLOWED_TOOLS: unknown tool(s) %s; valid tools: %s",
			strings.Join(unknown, ", "), strings.Join(valid, ", "))
	}

	if !readOnly && len(allowed) > 0 {
		logger.Warn("MCP_ALLOWED_TOOLS has no effect unless MCP_READ_ONLY=true; all tools are exposed")
	}
	if readOnly {
		logger.Info("Read-only mode enabled", "extra_allowed_tools", len(allowed))
	}

	return &toolPolicy{readOnly: readOnly, allowed: allowed}, nil
}

// parseAllowedTools splits a comma-separated tool list, trimming whitespace
// and dropping empty entries and duplicates.
func parseAllowedTools(s string) map[string]bool {
	allowed := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		if name := strings.TrimSpace(part); name != "" {
			allowed[name] = true
		}
	}
	return allowed
}

// allows reports whether the named tool is exposed. A nil policy (before
// SetupTools has run) allows everything.
func (p *toolPolicy) allows(name string) bool {
	if p == nil || !p.readOnly {
		return true
	}
	return readOnlyTools[name] || p.allowed[name]
}

// toolPolicyFilter hides disallowed tools from tools/list.
func (s *Server) toolPolicyFilter(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	filtered := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if s.toolPolicy.allows(tool.Name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// toolPolicyMiddleware rejects a direct tools/call for a disallowed tool
// before its handler runs. Hiding a tool from tools/list alone isn't access
// control: a client can still call it by name.
func (s *Server) toolPolicyMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if !s.toolPolicy.allows(req.Params.Name) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"tool %s is disabled in read-only mode (MCP_READ_ONLY=true); add it to MCP_ALLOWED_TOOLS to enable it",
				req.Params.Name)), nil
		}
		return next(ctx, req)
	}
}
