package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry"
	"pgregory.net/rapid"
)

// mutatingTools are the tools read-only mode must exclude unless they are
// explicitly approved.
var mutatingTools = []string{"opcua_write", "opcua_disconnect", "opcua_subscribe", "opcua_unsubscribe"}

// newPolicyTestServer builds a Server with read-only settings applied and
// SetupTools already run, so its policy reflects the real registered tools.
func newPolicyTestServer(t *testing.T, readOnly bool, allowedTools string) *Server {
	t.Helper()
	s := newTestServer(t)
	s.config.MCP.ReadOnly = readOnly
	s.config.MCP.AllowedTools = allowedTools
	if err := s.SetupTools(); err != nil {
		t.Fatalf("SetupTools() error: %v", err)
	}
	return s
}

// rpc sends one JSON-RPC request through mcp-go's real dispatcher, so tool
// filtering and middleware run exactly as they do for a live client.
func rpc(t *testing.T, s *Server, method string, params any) json.RawMessage {
	t.Helper()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp := s.mcpServer.HandleMessage(context.Background(), req)
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return out
}

func listedTools(t *testing.T, s *Server) []string {
	t.Helper()
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rpc(t, s, "tools/list", map[string]any{}), &resp); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	names := make([]string, 0, len(resp.Result.Tools))
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func callResult(t *testing.T, s *Server, name string, args map[string]any) (isError bool, text string) {
	t.Helper()
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	raw := rpc(t, s, "tools/call", map[string]any{"name": name, "arguments": args})
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal tools/call: %v", err)
	}
	if len(resp.Result.Content) == 0 {
		t.Fatalf("tools/call %s returned no content: %s", name, raw)
	}
	return resp.Result.IsError, resp.Result.Content[0].Text
}

// TestToolsListDiscovery covers tools/list under each mode: default exposes
// all 18 tools, read-only hides the mutating ones, and the allowlist brings
// back exactly the tools it names.
func TestToolsListDiscovery(t *testing.T) {
	tests := []struct {
		name         string
		readOnly     bool
		allowedTools string
		wantCount    int
		wantHidden   []string
		wantVisible  []string
	}{
		{name: "default exposes everything", wantCount: 18, wantVisible: mutatingTools},
		{name: "read-only hides mutating tools", readOnly: true, wantCount: 14, wantHidden: mutatingTools, wantVisible: []string{"opcua_read", "opcua_connect", "opcua_force_discovery", "opcua_list_subscriptions"}},
		{name: "allowlist re-enables named tools", readOnly: true, allowedTools: " opcua_write ,opcua_subscribe,,", wantCount: 16, wantHidden: []string{"opcua_disconnect", "opcua_unsubscribe"}, wantVisible: []string{"opcua_write", "opcua_subscribe"}},
		{name: "allowlist ignored without read-only", allowedTools: "opcua_write", wantCount: 18, wantVisible: mutatingTools},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newPolicyTestServer(t, tt.readOnly, tt.allowedTools)
			names := listedTools(t, s)
			if len(names) != tt.wantCount {
				t.Errorf("tools/list returned %d tools, want %d: %v", len(names), tt.wantCount, names)
			}
			for _, n := range tt.wantHidden {
				if slices.Contains(names, n) {
					t.Errorf("tools/list includes %s, want it hidden", n)
				}
			}
			for _, n := range tt.wantVisible {
				if !slices.Contains(names, n) {
					t.Errorf("tools/list is missing %s", n)
				}
			}
		})
	}
}

// TestDirectCallDenied covers a client calling a hidden tool by name: the
// middleware must reject it before the handler runs. The handlers here would
// otherwise answer with their own "not connected"/argument messages, so the
// denial text proves the handler never ran.
func TestDirectCallDenied(t *testing.T) {
	s := newPolicyTestServer(t, true, "")
	for _, name := range mutatingTools {
		t.Run(name, func(t *testing.T) {
			isError, text := callResult(t, s, name, map[string]any{"node_id": "i=85", "value": "1", "node_ids": "i=85", "subscription_id": "x"})
			if !isError {
				t.Fatalf("tools/call %s succeeded, want a denial", name)
			}
			if !strings.Contains(text, "disabled in read-only mode") || !strings.Contains(text, name) {
				t.Errorf("tools/call %s returned %q, want the read-only denial naming the tool", name, text)
			}
			if kind := classifyErrorText(text); kind != telemetry.ErrKindPermissionDenied {
				t.Errorf("denial classified as %q, want %q", kind, telemetry.ErrKindPermissionDenied)
			}
		})
	}
}

// TestDirectCallAllowed covers the other side: allowed tools reach their
// handler in read-only mode, including an allowlisted mutating tool.
func TestDirectCallAllowed(t *testing.T) {
	s := newPolicyTestServer(t, true, "opcua_disconnect")
	for _, name := range []string{"opcua_list_subscriptions", "opcua_disconnect"} {
		_, text := callResult(t, s, name, map[string]any{})
		if strings.Contains(text, "disabled in read-only mode") {
			t.Errorf("tools/call %s was denied, want it to reach its handler: %q", name, text)
		}
	}
}

// TestUnknownAllowedToolFailsSetup covers fail-closed validation: a typo in
// MCP_ALLOWED_TOOLS must stop startup instead of silently leaving the
// intended tool disabled.
func TestUnknownAllowedToolFailsSetup(t *testing.T) {
	s := newTestServer(t)
	s.config.MCP.ReadOnly = true
	s.config.MCP.AllowedTools = "opcua_write,opcua_wrte"
	err := s.SetupTools()
	if err == nil {
		t.Fatal("SetupTools() succeeded with an unknown allowlist entry, want an error")
	}
	if !strings.Contains(err.Error(), "opcua_wrte") || !strings.Contains(err.Error(), "opcua_read") {
		t.Errorf("error %q should name the unknown tool and list valid tools", err)
	}
}

// TestPresetCoversRegisteredTools keeps readOnlyTools honest: every preset
// entry must be a registered tool, and every registered tool must be either
// in the preset or one of the known mutating tools.
func TestPresetCoversRegisteredTools(t *testing.T) {
	s := newPolicyTestServer(t, false, "")
	for name := range readOnlyTools {
		if !slices.Contains(s.toolNames, name) {
			t.Errorf("readOnlyTools lists %s, which is not a registered tool", name)
		}
	}
	for _, name := range s.toolNames {
		if !readOnlyTools[name] && !slices.Contains(mutatingTools, name) {
			t.Errorf("registered tool %s is neither in the read-only preset nor a known mutating tool; classify it", name)
		}
	}
}

// TestToolPolicyMatchesSetModel (PBT-05 oracle, PBT-03 invariant) checks
// the policy against a plain set model for random modes and allowlists, and
// that the tools/list filter returns exactly the allowed subset of its input.
func TestToolPolicyMatchesSetModel(t *testing.T) {
	registered := newPolicyTestServer(t, false, "").toolNames

	rapid.Check(t, func(rt *rapid.T) {
		readOnly := rapid.Bool().Draw(rt, "readOnly")
		allow := rapid.SliceOfDistinct(rapid.SampledFrom(registered), rapid.ID[string]).Draw(rt, "allow")

		policy, err := newToolPolicy(readOnly, strings.Join(allow, ","), registered)
		if err != nil {
			rt.Fatalf("newToolPolicy() error: %v", err)
		}

		model := make(map[string]bool)
		for _, name := range registered {
			model[name] = !readOnly || readOnlyTools[name] || slices.Contains(allow, name)
		}
		for _, name := range registered {
			if policy.allows(name) != model[name] {
				rt.Fatalf("allows(%s) = %v, model says %v (readOnly=%v allow=%v)", name, policy.allows(name), model[name], readOnly, allow)
			}
		}
		for _, name := range mutatingTools {
			if readOnly && !slices.Contains(allow, name) && policy.allows(name) {
				rt.Fatalf("mutating tool %s allowed in read-only mode without approval", name)
			}
		}

		s := &Server{toolPolicy: policy}
		input := make([]mcp.Tool, 0, len(registered))
		for _, name := range registered {
			input = append(input, mcp.Tool{Name: name})
		}
		out := s.toolPolicyFilter(context.Background(), input)
		seen := make(map[string]bool)
		for _, tool := range out {
			if !model[tool.Name] {
				rt.Fatalf("filter returned disallowed tool %s", tool.Name)
			}
			seen[tool.Name] = true
		}
		for name, ok := range model {
			if ok && !seen[name] {
				rt.Fatalf("filter dropped allowed tool %s", name)
			}
		}
	})
}

// TestAllowlistParseIdempotent (PBT-04) checks parsing is insensitive to
// order, whitespace, empty entries and duplicates: rendering the parsed set
// back to a string and parsing it again yields the same set.
func TestAllowlistParseIdempotent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		names := rapid.SliceOf(rapid.StringMatching(`[a-z_]{1,12}`)).Draw(rt, "names")
		pads := rapid.SliceOfN(rapid.SampledFrom([]string{"", " ", "  ", "\t"}), len(names)+1, len(names)+1).Draw(rt, "pads")

		var sb strings.Builder
		for i, n := range names {
			fmt.Fprintf(&sb, "%s%s%s,", pads[i], n, pads[i])
		}
		fmt.Fprintf(&sb, "%s,", pads[len(names)])

		first := parseAllowedTools(sb.String())
		rendered := make([]string, 0, len(first))
		for n := range first {
			rendered = append(rendered, n)
		}
		second := parseAllowedTools(strings.Join(rendered, ","))

		if len(first) != len(second) {
			rt.Fatalf("parse not idempotent: %v vs %v", first, second)
		}
		for n := range first {
			if !second[n] {
				rt.Fatalf("parse not idempotent: %s lost on second parse", n)
			}
		}
		for _, n := range names {
			if !first[n] {
				rt.Fatalf("name %q dropped by parse", n)
			}
		}
	})
}
