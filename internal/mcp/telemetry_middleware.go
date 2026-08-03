package mcp

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry"
)

// telemetryMiddleware is registered once via server.WithToolHandlerMiddleware
// in NewServer, giving every one of the 18 registered tools RecordToolCall/
// RecordError coverage from a single place rather than a call added inside
// each handleX method. mcp-go's dispatcher (handleToolCall) only invokes
// registered middleware after resolving request.Params.Name against its own
// tool map, so by the time this runs, the name is always exactly one of this
// server's own registered tool name strings - never arbitrary client input.
func (s *Server) telemetryMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := next(ctx, req)

		s.telemetry.RecordToolCall(req.Params.Name, len(req.GetArguments()))

		switch {
		case err != nil:
			// A Go-level error from the handler itself (rare - most tool
			// handlers report failure via CallToolResult.IsError instead, see
			// below). Only its coarse category is ever recorded, never
			// err.Error() itself.
			s.telemetry.RecordError(classifyErrorText(err.Error()))
		case result != nil && result.IsError:
			s.telemetry.RecordError(classifyErrorText(resultText(result)))
		}

		return result, err
	}
}

// resultText concatenates a CallToolResult's text content, for classifyErrorText
// to inspect in-process. This text is never itself recorded or transmitted -
// only the category classifyErrorText derives from it.
func resultText(result *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// classifyErrorText maps a tool error's message to one of telemetry's fixed
// ErrKind* categories using simple keyword matching against this codebase's
// existing error message conventions (see e.g. handleWrite's "expects a
// ... value" hints, or the various "not connected"/"required" messages
// throughout server.go). It never returns the input text itself, and
// aggregator.recordError additionally clamps any result to the known set -
// this function only chooses which known category applies.
func classifyErrorText(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"), strings.Contains(lower, "timed out"):
		return telemetry.ErrKindTimeout
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "no route to host"), strings.Contains(lower, "not connected"):
		return telemetry.ErrKindConnectionRefused
	case strings.Contains(lower, "invalid json"), strings.Contains(lower, "expects a"), strings.Contains(lower, "data type"):
		return telemetry.ErrKindTypeMismatch
	case strings.Contains(lower, "required"), strings.Contains(lower, "failed to parse"), strings.Contains(lower, "invalid"):
		return telemetry.ErrKindInvalidArgument
	case strings.Contains(lower, "not found"), strings.Contains(lower, "no results returned"):
		return telemetry.ErrKindNotFound
	case strings.Contains(lower, "not writable"), strings.Contains(lower, "not user writable"), strings.Contains(lower, "permission"):
		return telemetry.ErrKindPermissionDenied
	case strings.Contains(lower, "unavailable"):
		return telemetry.ErrKindUnavailable
	default:
		return telemetry.ErrKindOther
	}
}
