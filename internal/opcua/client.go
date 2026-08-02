package opcua

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// opcuaClient is the subset of *opcua.Client's surface this package depends
// on. Depending on this interface rather than the concrete gopcua type lets
// tests inject a mock that returns canned responses, without needing a live
// or simulated OPC-UA server.
type opcuaClient interface {
	Connect(ctx context.Context) error
	Close(ctx context.Context) error
	Read(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error)
	Write(ctx context.Context, req *ua.WriteRequest) (*ua.WriteResponse, error)
	Browse(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error)
	BrowseNext(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error)
	Subscribe(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (*opcua.Subscription, error)
}

// var _ opcuaClient documents (and fails to compile if broken by an upstream
// gopcua signature change) that *opcua.Client satisfies opcuaClient.
var _ opcuaClient = (*opcua.Client)(nil)

// Client wraps the OPC-UA client with additional functionality
type Client struct {
	mu        sync.RWMutex
	client    opcuaClient
	config    *config.OPCUAConfig // immutable post-construction; not guarded by mu
	connected bool
	stateCh   chan<- opcua.ConnState // registered via SetStateChangeChannel; nil unless a caller (ReconnectWatcher) wants state-change notifications
}

// snapshot returns a consistent read of the current client/connected state.
func (c *Client) snapshot() (opcuaClient, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client, c.connected
}

// NewClient creates a new OPC-UA client with the given configuration
func NewClient(cfg *config.OPCUAConfig) *Client {
	return &Client{
		config: cfg,
	}
}

// Connect establishes a connection to the OPC-UA server
func (c *Client) Connect(ctx context.Context) error {
	// Create endpoint URL
	endpoint := c.config.Endpoint

	// Create client options
	opts := []opcua.Option{
		opcua.RequestTimeout(c.config.RequestTimeout),
		opcua.SessionTimeout(c.config.SessionTimeout),
	}

	// Include the registered state-change channel (if any) on every
	// (re)connect - Connect always constructs a brand-new underlying gopcua
	// client, so gopcua's StateChangedCh option must be supplied again each
	// time rather than just once at initial construction.
	c.mu.RLock()
	stateCh := c.stateCh
	c.mu.RUnlock()
	if stateCh != nil {
		opts = append(opts, opcua.StateChangedCh(stateCh))
	}

	// Add security policy and mode
	if c.config.SecurityPolicy != "None" {
		opts = append(opts, opcua.SecurityPolicy(c.config.SecurityPolicy))
	}

	if c.config.SecurityMode != "None" {
		opts = append(opts, opcua.SecurityMode(ua.MessageSecurityModeFromString(c.config.SecurityMode)))
	}

	// Add authentication based on mode
	switch c.config.AuthMode {
	case "anonymous":
		opts = append(opts, opcua.AuthAnonymous())
	case "username":
		opts = append(opts, opcua.AuthUsername(c.config.Username, c.config.Password))
	case "certificate":
		var err error
		opts, err = c.addCertificateAuth(opts)
		if err != nil {
			return fmt.Errorf("failed to add certificate authentication: %w", err)
		}
	default:
		return fmt.Errorf("unsupported authentication mode: %s", c.config.AuthMode)
	}

	// Create client
	client, err := opcua.NewClient(endpoint, opts...)
	if err != nil {
		return fmt.Errorf("failed to create OPC-UA client: %w", err)
	}

	// Connect with retry logic
	for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
		if err := client.Connect(ctx); err != nil {
			if attempt < c.config.MaxRetries-1 {
				time.Sleep(c.config.RetryDelay)
				continue
			}
			return fmt.Errorf("failed to connect to OPC-UA server after %d attempts: %w", c.config.MaxRetries, err)
		}
		break
	}

	c.mu.Lock()
	c.client = client
	c.connected = true
	c.mu.Unlock()
	return nil
}

// addCertificateAuth adds certificate-based authentication to the client options
func (c *Client) addCertificateAuth(opts []opcua.Option) ([]opcua.Option, error) {
	// Load client certificate
	cert, err := tls.LoadX509KeyPair(c.config.CertFile, c.config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Add certificate and private key to client options
	opts = append(opts, opcua.Certificate(cert.Certificate[0]))

	// Type assert private key to RSA private key
	rsaPrivateKey, ok := cert.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not an RSA private key")
	}
	opts = append(opts, opcua.PrivateKey(rsaPrivateKey))

	// Add certificate authentication
	opts = append(opts, opcua.AuthCertificate(cert.Certificate[0]))

	return opts, nil
}

// SetStateChangeChannel registers ch to receive gopcua connection state
// changes on every future (re)connect. Connect() includes
// opcua.StateChangedCh(ch) in its options whenever ch is non-nil.
func (c *Client) SetStateChangeChannel(ch chan<- opcua.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stateCh = ch
}

// Subscribe creates a new OPC-UA subscription and returns it as the narrow
// SubscriptionHandle interface (defined in subscription.go) rather than the
// concrete *opcua.Subscription, so SubscriptionManager's tests can inject a
// mock in its place.
func (c *Client) Subscribe(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (SubscriptionHandle, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return nil, fmt.Errorf("client is not connected")
	}

	sub, err := client.Subscribe(ctx, params, notifyCh)
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}
	return sub, nil
}

// Disconnect closes the connection to the OPC-UA server
func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil && c.connected {
		err := c.client.Close(ctx)
		c.connected = false
		return err
	}
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	client, connected := c.snapshot()
	return connected && client != nil
}

// SetConnectedForTesting sets the connected flag for testing purposes
func (c *Client) SetConnectedForTesting(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = connected
}

// ParseNodeIDs parses a string containing one or more node IDs and returns a slice of parsed node IDs
// Supports multiple formats:
// - Single node ID: "i=85"
// - Comma-separated: "i=85,i=86,i=87"
// - JSON array: "[\"i=85\",\"i=86\",\"i=87\"]"
// - Mixed formats with proper handling of quoted strings
func ParseNodeIDs(input string) ([]string, error) {
	if input == "" {
		return nil, fmt.Errorf("empty node ID input")
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check for whitespace-only input
	if input == "" {
		return nil, fmt.Errorf("whitespace-only node ID input")
	}

	// Try to parse as JSON array first
	if strings.HasPrefix(input, "[") {
		var nodeIDs []string
		err := json.Unmarshal([]byte(input), &nodeIDs)
		if err == nil {
			// Validate each node ID
			for i, nodeID := range nodeIDs {
				nodeID = strings.TrimSpace(nodeID)
				if nodeID == "" {
					return nil, fmt.Errorf("empty node ID at position %d", i)
				}
				if _, err := ua.ParseNodeID(nodeID); err != nil {
					return nil, fmt.Errorf("invalid node ID '%s' at position %d: %w", nodeID, i, err)
				}
				nodeIDs[i] = nodeID
			}
			return nodeIDs, nil
		}
		// If JSON parsing fails, return error for malformed JSON
		return nil, fmt.Errorf("malformed JSON array: %w", err)
	}

	// Try comma-separated parsing
	if strings.Contains(input, ",") {
		parts := strings.Split(input, ",")
		var nodeIDs []string
		for i, part := range parts {
			part = strings.TrimSpace(part)
			// Remove quotes if present
			part = strings.Trim(part, "\"'")
			if part == "" {
				return nil, fmt.Errorf("empty node ID at position %d", i)
			}
			if _, err := ua.ParseNodeID(part); err != nil {
				return nil, fmt.Errorf("invalid node ID '%s' at position %d: %w", part, i, err)
			}
			nodeIDs = append(nodeIDs, part)
		}
		return nodeIDs, nil
	}

	// Single node ID
	input = strings.Trim(input, "\"'")
	if _, err := ua.ParseNodeID(input); err != nil {
		return nil, fmt.Errorf("invalid node ID '%s': %w", input, err)
	}

	return []string{input}, nil
}

// Read reads values from the specified node IDs
func (c *Client) Read(ctx context.Context, nodeIDs []string) ([]*ua.DataValue, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return nil, fmt.Errorf("client is not connected")
	}

	// Convert string node IDs to ua.NodeID
	ids := make([]*ua.NodeID, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		id, err := ua.ParseNodeID(nodeID)
		if err != nil {
			return nil, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
		}
		ids[i] = id
	}

	// Create read request
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead:        make([]*ua.ReadValueID, len(ids)),
	}

	for i, id := range ids {
		req.NodesToRead[i] = &ua.ReadValueID{
			NodeID:      id,
			AttributeID: ua.AttributeIDValue,
		}
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read from OPC-UA server: %w", err)
	}

	// Return all per-node results as-is, including bad-status ones: one
	// bad node must not discard the good results in the same batch
	// (findings.md H3). Only a transport-level failure (handled above)
	// produces a top-level error.
	return resp.Results, nil
}

// Write writes values to the specified node IDs with intelligent type conversion
func (c *Client) Write(ctx context.Context, nodeID string, value interface{}) error {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Get node type information for intelligent conversion
	typeInfo, err := c.GetNodeTypeInfo(ctx, nodeID)
	if err != nil {
		// If we can't get type info, fall back to basic conversion
		logger.Warn("Failed to get node type information, using basic conversion", "error", err)
		return c.writeWithBasicConversion(ctx, client, id, nodeID, value)
	}

	// Validate value against node's data type before attempting to write. An
	// invalid value must never reach a live OPC-UA device (findings.md,
	// "Write() silently ignores validation failures").
	if err := c.ValidateValueForNode(value, typeInfo); err != nil {
		return fmt.Errorf("value validation failed for node %s: %w", nodeID, err)
	}

	// Convert value to proper OPC-UA type based on node's data type
	convertedValue, err := c.convertValueToOPCUAType(value, typeInfo)
	if err != nil {
		return fmt.Errorf("failed to convert value to OPC-UA type: %w", err)
	}

	// Convert converted value to ua.Variant
	variant, err := ua.NewVariant(convertedValue)
	if err != nil {
		return fmt.Errorf("failed to convert value to variant: %w", err)
	}

	// Create write request
	req := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDValue,
				Value: &ua.DataValue{
					Value:        variant,
					EncodingMask: ua.DataValueValue,
				},
			},
		},
	}

	// Send write request
	resp, err := client.Write(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write to OPC-UA server: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0] != ua.StatusOK {
		return fmt.Errorf("write failed for node %s: %s", nodeID, resp.Results[0])
	}

	return nil
}

// Browse browses the node hierarchy starting from the specified node
func (c *Client) Browse(ctx context.Context, nodeID string) ([]*ua.ReferenceDescription, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return nil, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create browse request
	req := &ua.BrowseRequest{
		RequestedMaxReferencesPerNode: 1000,
		NodesToBrowse: []*ua.BrowseDescription{
			{
				NodeID:          id,
				BrowseDirection: ua.BrowseDirectionForward,
				ReferenceTypeID: ua.NewNumericNodeID(0, 33), // HierarchicalReferences
				IncludeSubtypes: true,
				NodeClassMask:   uint32(ua.NodeClassAll),
				ResultMask:      uint32(ua.BrowseResultMaskAll),
			},
		},
	}

	// Send browse request
	resp, err := client.Browse(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to browse OPC-UA server: %w", err)
	}

	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no browse results returned")
	}

	// Check for errors in response
	if resp.Results[0].StatusCode != ua.StatusOK {
		return nil, fmt.Errorf("browse failed for node %s: %s", nodeID, resp.Results[0].StatusCode)
	}

	references := append([]*ua.ReferenceDescription(nil), resp.Results[0].References...)
	continuationPoint := resp.Results[0].ContinuationPoint

	// Follow BrowseNext continuation points so a node with more references
	// than RequestedMaxReferencesPerNode isn't silently truncated
	// (findings.md H5). Bounded by both ctx and a hard iteration cap so a
	// misbehaving server that never exhausts its continuation point can't
	// hang this call forever.
	for iterations := 0; len(continuationPoint) > 0; iterations++ {
		if iterations >= maxBrowseNextIterations {
			return nil, fmt.Errorf("browse for node %s exceeded %d BrowseNext continuations without exhausting results", nodeID, maxBrowseNextIterations)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("browse for node %s cancelled while following continuation points: %w", nodeID, err)
		}

		nextResp, err := client.BrowseNext(ctx, &ua.BrowseNextRequest{
			ContinuationPoints: [][]byte{continuationPoint},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to continue browse for node %s: %w", nodeID, err)
		}
		if len(nextResp.Results) == 0 {
			return nil, fmt.Errorf("no BrowseNext results returned for node %s", nodeID)
		}

		next := nextResp.Results[0]
		if next.StatusCode != ua.StatusOK {
			return nil, fmt.Errorf("BrowseNext failed for node %s: %s", nodeID, next.StatusCode)
		}

		references = append(references, next.References...)
		continuationPoint = next.ContinuationPoint
	}

	return references, nil
}

// maxBrowseNextIterations bounds the BrowseNext continuation loop in Browse,
// independent of ctx's deadline, so a misbehaving or malicious server that
// never returns an empty ContinuationPoint can't cause an unbounded loop.
const maxBrowseNextIterations = 10000

// GetNodeClass returns the node class of the specified node
func (c *Client) GetNodeClass(ctx context.Context, nodeID string) (ua.NodeClass, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return 0, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return 0, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for node class
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDNodeClass,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to read node class: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return 0, fmt.Errorf("read node class failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return 0, fmt.Errorf("no results returned")
	}

	// Extract node class from result
	return decodeNodeClass(resp.Results[0].Value.Value())
}

// decodeNodeClass converts the raw NodeClass attribute value into a ua.NodeClass.
// NodeClass decodes on the wire as a plain int32 (gopcua ua/variant.go's
// TypeIDInt32 -> buf.ReadInt32()), not the uint32-based ua.NodeClass type
// itself, so asserting directly to ua.NodeClass always fails against a real
// server (findings.md H10a).
func decodeNodeClass(raw interface{}) (ua.NodeClass, error) {
	v, ok := raw.(int32)
	if !ok {
		return 0, fmt.Errorf("failed to extract node class from result: unexpected type %T", raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("failed to extract node class from result: invalid value %d", v)
	}
	return ua.NodeClass(uint32(v)), nil
}

// GetNodeDataType returns the data type information for a variable node
func (c *Client) GetNodeDataType(ctx context.Context, nodeID string) (*ua.NodeID, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return nil, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for data type
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDDataType,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read node data type: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return nil, fmt.Errorf("read node data type failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

	// Extract data type from result
	dataType, ok := resp.Results[0].Value.Value().(*ua.NodeID)
	if !ok {
		return nil, fmt.Errorf("failed to extract data type from result")
	}

	return dataType, nil
}

// GetNodeValueRank returns the value rank information for a variable node
func (c *Client) GetNodeValueRank(ctx context.Context, nodeID string) (int32, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return 0, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return 0, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for value rank
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDValueRank,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to read node value rank: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return 0, fmt.Errorf("read node value rank failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return 0, fmt.Errorf("no results returned")
	}

	// Extract value rank from result
	valueRank, ok := resp.Results[0].Value.Value().(int32)
	if !ok {
		return 0, fmt.Errorf("failed to extract value rank from result")
	}

	return valueRank, nil
}

// GetNodeArrayDimensions returns the array dimensions for a variable node
func (c *Client) GetNodeArrayDimensions(ctx context.Context, nodeID string) ([]uint32, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return nil, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for array dimensions
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDArrayDimensions,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read node array dimensions: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return nil, fmt.Errorf("read node array dimensions failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

	// Extract array dimensions from result
	arrayDimensions, ok := resp.Results[0].Value.Value().([]uint32)
	if !ok {
		// Array dimensions might be nil for scalar values
		return nil, nil
	}

	return arrayDimensions, nil
}

// GetNodeAccessLevel returns the access level for a variable node
func (c *Client) GetNodeAccessLevel(ctx context.Context, nodeID string) (byte, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return 0, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return 0, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for access level
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDAccessLevel,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to read node access level: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return 0, fmt.Errorf("read node access level failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return 0, fmt.Errorf("no results returned")
	}

	// Extract access level from result
	accessLevel, ok := resp.Results[0].Value.Value().(byte)
	if !ok {
		return 0, fmt.Errorf("failed to extract access level from result")
	}

	return accessLevel, nil
}

// GetNodeUserAccessLevel returns the user access level for a variable node
func (c *Client) GetNodeUserAccessLevel(ctx context.Context, nodeID string) (byte, error) {
	client, connected := c.snapshot()
	if !connected || client == nil {
		return 0, fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return 0, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Create read request for user access level
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDUserAccessLevel,
			},
		},
	}

	// Send read request
	resp, err := client.Read(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to read node user access level: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return 0, fmt.Errorf("read node user access level failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return 0, fmt.Errorf("no results returned")
	}

	// Extract user access level from result
	userAccessLevel, ok := resp.Results[0].Value.Value().(byte)
	if !ok {
		return 0, fmt.Errorf("failed to extract user access level from result")
	}

	return userAccessLevel, nil
}

const (
	serverStatusNodeID = 2256
	writeAccessBit     = 0x02
)

// GetServerInfo returns information about the OPC-UA server
func (c *Client) GetServerInfo(ctx context.Context) (*ua.ServerStatusDataType, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("client is not connected")
	}

	results, err := c.Read(ctx, []string{fmt.Sprintf("i=%d", serverStatusNodeID)})
	if err != nil {
		return nil, fmt.Errorf("failed to read server status: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

	result := results[0]
	if result.Status != ua.StatusOK {
		return nil, fmt.Errorf("read server status failed: %s", result.Status)
	}

	return c.extractServerStatus(result.Value.Value())
}

func (c *Client) extractServerStatus(value interface{}) (*ua.ServerStatusDataType, error) {
	if serverStatus, ok := value.(*ua.ServerStatusDataType); ok {
		return serverStatus, nil
	}

	valueMap, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected value type")
	}

	serverStatusMap, ok := valueMap["Value"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid server status structure")
	}

	serverStatus := &ua.ServerStatusDataType{
		StartTime:   c.extractTime(serverStatusMap, "StartTime"),
		CurrentTime: c.extractTime(serverStatusMap, "CurrentTime"),
		State:       ua.ServerState(c.extractUint32(serverStatusMap, "State")),
		BuildInfo:   c.extractBuildInfo(serverStatusMap),
	}

	return serverStatus, nil
}

func (c *Client) extractTime(m map[string]interface{}, key string) time.Time {
	if t, ok := m[key].(time.Time); ok {
		return t
	}
	return time.Time{}
}

func (c *Client) extractUint32(m map[string]interface{}, key string) uint32 {
	if v, ok := m[key].(uint32); ok {
		return v
	}
	return 0
}

func (c *Client) extractString(m map[string]interface{}, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func (c *Client) extractBuildInfo(m map[string]interface{}) *ua.BuildInfo {
	buildInfoMap, ok := m["BuildInfo"].(map[string]interface{})
	if !ok {
		return nil
	}

	return &ua.BuildInfo{
		ProductURI:       c.extractString(buildInfoMap, "ProductURI"),
		ManufacturerName: c.extractString(buildInfoMap, "ManufacturerName"),
		ProductName:      c.extractString(buildInfoMap, "ProductName"),
		SoftwareVersion:  c.extractString(buildInfoMap, "SoftwareVersion"),
		BuildNumber:      c.extractString(buildInfoMap, "BuildNumber"),
		BuildDate:        c.extractTime(buildInfoMap, "BuildDate"),
	}
}

// NodeTypeInfo contains information about a node's data type and structure
type NodeTypeInfo struct {
	DataType        *ua.NodeID
	ValueRank       int32
	ArrayDimensions []uint32
	IsArray         bool
	IsScalar        bool
	AccessLevel     byte
	UserAccessLevel byte
	IsWritable      bool
	IsUserWritable  bool
}

// GetNodeTypeInfo retrieves comprehensive type information for a node
func (c *Client) GetNodeTypeInfo(ctx context.Context, nodeID string) (*NodeTypeInfo, error) {
	// Get data type
	dataType, err := c.GetNodeDataType(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get data type: %w", err)
	}

	// Get value rank
	valueRank, err := c.GetNodeValueRank(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get value rank: %w", err)
	}

	// Get array dimensions (optional - some nodes don't support this attribute)
	arrayDimensions, err := c.GetNodeArrayDimensions(ctx, nodeID)
	if err != nil {
		// Array dimensions are optional for some nodes, continue with empty slice
		arrayDimensions = []uint32{}
	}

	// Get access levels
	accessLevel, err := c.GetNodeAccessLevel(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get access level: %w", err)
	}

	userAccessLevel, err := c.GetNodeUserAccessLevel(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user access level: %w", err)
	}

	isWritable := (accessLevel & writeAccessBit) != 0
	isUserWritable := (userAccessLevel & writeAccessBit) != 0

	return &NodeTypeInfo{
		DataType:        dataType,
		ValueRank:       valueRank,
		ArrayDimensions: arrayDimensions,
		IsArray:         valueRank > 0,
		IsScalar:        valueRank == -1 || valueRank == 0,
		AccessLevel:     accessLevel,
		UserAccessLevel: userAccessLevel,
		IsWritable:      isWritable,
		IsUserWritable:  isUserWritable,
	}, nil
}

// ValidateValueForNode validates that a value is compatible with the given,
// already-fetched node type information. Callers that already have a
// NodeTypeInfo (e.g. Write(), which fetches it once) must pass it directly
// instead of triggering a second, redundant 5-read round trip (findings.md
// H4; the full fix, a persistent typeinfo cache, lands with P2-8).
func (c *Client) ValidateValueForNode(value interface{}, typeInfo *NodeTypeInfo) error {
	// Check if node is writable
	if !typeInfo.IsUserWritable {
		return fmt.Errorf("node is not writable (UserAccessLevel: %d, AccessLevel: %d)",
			typeInfo.UserAccessLevel, typeInfo.AccessLevel)
	}

	// Validate based on value rank
	if typeInfo.IsArray {
		return c.validateArrayValue(value, typeInfo)
	}
	return c.validateScalarValue(value, typeInfo)
}

// validateScalarValue validates a scalar value against the node's data type
func (c *Client) validateScalarValue(value interface{}, typeInfo *NodeTypeInfo) error {
	// Get the data type identifier
	dataTypeID := typeInfo.DataType.IntID()

	// Validate based on common OPC-UA data types
	switch dataTypeID {
	case uint32(ua.TypeIDBoolean): // Boolean
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean value, got %T", value)
		}
	case uint32(ua.TypeIDSByte): // SByte
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for SByte, got %T", value)
		}
		if !c.isInRange(value, -128, 127) {
			return fmt.Errorf("SByte value out of range (-128 to 127)")
		}
	case uint32(ua.TypeIDByte): // Byte
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Byte, got %T", value)
		}
		if !c.isInRange(value, 0, 255) {
			return fmt.Errorf("Byte value out of range (0 to 255)")
		}
	case uint32(ua.TypeIDInt16): // Int16
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int16, got %T", value)
		}
		if !c.isInRange(value, -32768, 32767) {
			return fmt.Errorf("Int16 value out of range (-32768 to 32767)")
		}
	case uint32(ua.TypeIDUint16): // UInt16
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt16, got %T", value)
		}
		if !c.isInRange(value, 0, 65535) {
			return fmt.Errorf("UInt16 value out of range (0 to 65535)")
		}
	case uint32(ua.TypeIDInt32): // Int32
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int32, got %T", value)
		}
		if !c.isInRange(value, -2147483648, 2147483647) {
			return fmt.Errorf("Int32 value out of range (-2147483648 to 2147483647)")
		}
	case uint32(ua.TypeIDUint32): // UInt32
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt32, got %T", value)
		}
		if !c.isInRange(value, 0, 4294967295) {
			return fmt.Errorf("UInt32 value out of range (0 to 4294967295)")
		}
	case uint32(ua.TypeIDInt64): // Int64
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int64, got %T", value)
		}
	case uint32(ua.TypeIDUint64): // UInt64
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt64, got %T", value)
		}
		if !c.isNonNegative(value) {
			return fmt.Errorf("UInt64 value must be non-negative")
		}
	case uint32(ua.TypeIDFloat): // Float
		if !c.isFloatType(value) {
			return fmt.Errorf("expected float value, got %T", value)
		}
	case uint32(ua.TypeIDDouble): // Double
		if !c.isFloatType(value) {
			return fmt.Errorf("expected float value, got %T", value)
		}
	case uint32(ua.TypeIDString): // String
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value, got %T", value)
		}
	case uint32(ua.TypeIDDateTime): // DateTime
		if !c.isDateTimeType(value) {
			return fmt.Errorf("expected DateTime value (string or number), got %T", value)
		}
	case uint32(ua.TypeIDGUID): // Guid
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value for Guid, got %T", value)
		}
	case uint32(ua.TypeIDByteString): // ByteString
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value for ByteString, got %T", value)
		}
	default:
		// For unknown data types, we'll be more permissive but log a warning
		// This allows for custom data types that we don't explicitly handle
		return nil
	}

	return nil
}

// validateArrayValue validates an array value against the node's data type
func (c *Client) validateArrayValue(value interface{}, typeInfo *NodeTypeInfo) error {
	// Check if value is a slice/array
	valueSlice, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("expected array value, got %T", value)
	}

	// Validate array dimensions if specified
	if len(typeInfo.ArrayDimensions) > 0 {
		expectedLength := int(typeInfo.ArrayDimensions[0])
		if len(valueSlice) != expectedLength {
			return fmt.Errorf("expected array length %d, got %d", expectedLength, len(valueSlice))
		}
	}

	// Validate each element in the array
	for i, element := range valueSlice {
		if err := c.validateScalarValue(element, typeInfo); err != nil {
			return fmt.Errorf("array element %d validation failed: %w", i, err)
		}
	}

	return nil
}

// Helper functions for type validation
func (c *Client) isIntegerType(value interface{}) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float64:
		return true
	default:
		return false
	}
}

func (c *Client) isFloatType(value interface{}) bool {
	switch value.(type) {
	case float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func (c *Client) isDateTimeType(value interface{}) bool {
	switch value.(type) {
	case string, int, int64, float64:
		return true
	default:
		return false
	}
}

func (c *Client) isInRange(value interface{}, min, max int64) bool {
	switch v := value.(type) {
	case int:
		return int64(v) >= min && int64(v) <= max
	case int8:
		return int64(v) >= min && int64(v) <= max
	case int16:
		return int64(v) >= min && int64(v) <= max
	case int32:
		return int64(v) >= min && int64(v) <= max
	case int64:
		return v >= min && v <= max
	case uint:
		return int64(v) >= min && int64(v) <= max
	case uint8:
		return int64(v) >= min && int64(v) <= max
	case uint16:
		return int64(v) >= min && int64(v) <= max
	case uint32:
		return int64(v) >= min && int64(v) <= max
	case uint64:
		return int64(v) >= min && int64(v) <= max
	case float64:
		return int64(v) >= min && int64(v) <= max
	default:
		return false
	}
}

func (c *Client) isNonNegative(value interface{}) bool {
	switch v := value.(type) {
	case int:
		return v >= 0
	case int8:
		return v >= 0
	case int16:
		return v >= 0
	case int32:
		return v >= 0
	case int64:
		return v >= 0
	case uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		return v >= 0
	default:
		return false
	}
}

// writeWithBasicConversion performs a write operation using basic type conversion (fallback)
func (c *Client) writeWithBasicConversion(ctx context.Context, client opcuaClient, id *ua.NodeID, nodeID string, value interface{}) error {
	// Convert value to ua.Variant using basic conversion
	variant, err := ua.NewVariant(value)
	if err != nil {
		return fmt.Errorf("failed to convert value to variant: %w", err)
	}

	// Create write request
	req := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{
			{
				NodeID:      id,
				AttributeID: ua.AttributeIDValue,
				Value: &ua.DataValue{
					Value:        variant,
					EncodingMask: ua.DataValueValue,
				},
			},
		},
	}

	// Send write request
	resp, err := client.Write(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write to OPC-UA server: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0] != ua.StatusOK {
		return fmt.Errorf("write failed for node %s: %s", nodeID, resp.Results[0])
	}

	return nil
}

// convertValueToOPCUAType converts a value to the proper OPC-UA type based on node type information
func (c *Client) convertValueToOPCUAType(value interface{}, typeInfo *NodeTypeInfo) (interface{}, error) {
	// Get the data type identifier
	dataTypeID := typeInfo.DataType.IntID()

	// Handle arrays
	if typeInfo.IsArray {
		return c.convertArrayValue(value, typeInfo, dataTypeID)
	}

	// Handle scalar values
	return c.convertScalarValue(value, dataTypeID)
}

// convertScalarValue converts a scalar value to the proper OPC-UA type
func (c *Client) convertScalarValue(value interface{}, dataTypeID uint32) (interface{}, error) {
	switch dataTypeID {
	case uint32(ua.TypeIDBoolean): // Boolean
		return c.convertToBoolean(value)
	case uint32(ua.TypeIDSByte): // SByte
		return c.convertToSByte(value)
	case uint32(ua.TypeIDByte): // Byte
		return c.convertToByte(value)
	case uint32(ua.TypeIDInt16): // Int16
		return c.convertToInt16(value)
	case uint32(ua.TypeIDUint16): // UInt16
		return c.convertToUInt16(value)
	case uint32(ua.TypeIDInt32): // Int32
		return c.convertToInt32(value)
	case uint32(ua.TypeIDUint32): // UInt32
		return c.convertToUInt32(value)
	case uint32(ua.TypeIDInt64): // Int64
		return c.convertToInt64(value)
	case uint32(ua.TypeIDUint64): // UInt64
		return c.convertToUInt64(value)
	case uint32(ua.TypeIDFloat): // Float
		return c.convertToFloat(value)
	case uint32(ua.TypeIDDouble): // Double
		return c.convertToDouble(value)
	case uint32(ua.TypeIDString): // String
		return c.convertToString(value)
	case uint32(ua.TypeIDDateTime): // DateTime
		return c.convertToDateTime(value)
	case uint32(ua.TypeIDGUID): // Guid
		return c.convertToString(value) // Guid is represented as string
	case uint32(ua.TypeIDByteString): // ByteString
		return c.convertToString(value) // ByteString is represented as string
	default:
		// For unknown data types, return the original value
		logger.Warn("Unknown data type, using original value", "dataTypeID", dataTypeID)
		return value, nil
	}
}

// convertArrayValue converts an array value to the proper OPC-UA type
func (c *Client) convertArrayValue(value interface{}, typeInfo *NodeTypeInfo, dataTypeID uint32) (interface{}, error) {
	// Check if value is a slice/array
	valueSlice, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array value, got %T", value)
	}

	// Convert each element in the array
	convertedArray := make([]interface{}, len(valueSlice))
	for i, element := range valueSlice {
		convertedElement, err := c.convertScalarValue(element, dataTypeID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert array element %d: %w", i, err)
		}
		convertedArray[i] = convertedElement
	}

	return convertedArray, nil
}

// Type conversion helper functions
func (c *Client) convertToBoolean(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(v) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("invalid boolean string: %s", v)
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return c.toInt64(v) != 0, nil
	case float32, float64:
		return c.toFloat64(v) != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to boolean", value)
	}
}

func (c *Client) convertToSByte(value interface{}) (int8, error) {
	switch v := value.(type) {
	case int8:
		return v, nil
	case int, int16, int32, int64:
		val := c.toInt64(v)
		if val < -128 || val > 127 {
			return 0, fmt.Errorf("value %d out of range for SByte (-128 to 127)", val)
		}
		return int8(val), nil
	case uint, uint8, uint16, uint32, uint64:
		val := c.toUInt64(v)
		if val > 127 {
			return 0, fmt.Errorf("value %d out of range for SByte (-128 to 127)", val)
		}
		return int8(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < -128 || val > 127 {
			return 0, fmt.Errorf("value %d out of range for SByte (-128 to 127)", val)
		}
		return int8(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < -128 || val > 127 {
			return 0, fmt.Errorf("value %d out of range for SByte (-128 to 127)", val)
		}
		return int8(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to SByte", value)
	}
}

func (c *Client) convertToByte(value interface{}) (uint8, error) {
	switch v := value.(type) {
	case uint8:
		return v, nil
	case int, int8, int16, int32, int64:
		val := c.toInt64(v)
		if val < 0 || val > 255 {
			return 0, fmt.Errorf("value %d out of range for Byte (0 to 255)", val)
		}
		return uint8(val), nil
	case uint, uint16, uint32, uint64:
		val := c.toUInt64(v)
		if val > 255 {
			return 0, fmt.Errorf("value %d out of range for Byte (0 to 255)", val)
		}
		return uint8(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < 0 || val > 255 {
			return 0, fmt.Errorf("value %d out of range for Byte (0 to 255)", val)
		}
		return uint8(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < 0 || val > 255 {
			return 0, fmt.Errorf("value %d out of range for Byte (0 to 255)", val)
		}
		return uint8(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to Byte", value)
	}
}

func (c *Client) convertToInt16(value interface{}) (int16, error) {
	switch v := value.(type) {
	case int16:
		return v, nil
	case int, int8, int32, int64:
		val := c.toInt64(v)
		if val < -32768 || val > 32767 {
			return 0, fmt.Errorf("value %d out of range for Int16 (-32768 to 32767)", val)
		}
		return int16(val), nil
	case uint, uint8, uint16, uint32, uint64:
		val := c.toUInt64(v)
		if val > 32767 {
			return 0, fmt.Errorf("value %d out of range for Int16 (-32768 to 32767)", val)
		}
		return int16(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < -32768 || val > 32767 {
			return 0, fmt.Errorf("value %d out of range for Int16 (-32768 to 32767)", val)
		}
		return int16(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < -32768 || val > 32767 {
			return 0, fmt.Errorf("value %d out of range for Int16 (-32768 to 32767)", val)
		}
		return int16(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to Int16", value)
	}
}

func (c *Client) convertToUInt16(value interface{}) (uint16, error) {
	switch v := value.(type) {
	case uint16:
		return v, nil
	case int, int8, int16, int32, int64:
		val := c.toInt64(v)
		if val < 0 || val > 65535 {
			return 0, fmt.Errorf("value %d out of range for UInt16 (0 to 65535)", val)
		}
		return uint16(val), nil
	case uint, uint8, uint32, uint64:
		val := c.toUInt64(v)
		if val > 65535 {
			return 0, fmt.Errorf("value %d out of range for UInt16 (0 to 65535)", val)
		}
		return uint16(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < 0 || val > 65535 {
			return 0, fmt.Errorf("value %d out of range for UInt16 (0 to 65535)", val)
		}
		return uint16(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < 0 || val > 65535 {
			return 0, fmt.Errorf("value %d out of range for UInt16 (0 to 65535)", val)
		}
		return uint16(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to UInt16", value)
	}
}

func (c *Client) convertToInt32(value interface{}) (int32, error) {
	switch v := value.(type) {
	case int32:
		return v, nil
	case int, int8, int16, int64:
		val := c.toInt64(v)
		if val < -2147483648 || val > 2147483647 {
			return 0, fmt.Errorf("value %d out of range for Int32 (-2147483648 to 2147483647)", val)
		}
		return int32(val), nil
	case uint, uint8, uint16, uint32, uint64:
		val := c.toUInt64(v)
		if val > 2147483647 {
			return 0, fmt.Errorf("value %d out of range for Int32 (-2147483648 to 2147483647)", val)
		}
		return int32(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < -2147483648 || val > 2147483647 {
			return 0, fmt.Errorf("value %d out of range for Int32 (-2147483648 to 2147483647)", val)
		}
		return int32(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < -2147483648 || val > 2147483647 {
			return 0, fmt.Errorf("value %d out of range for Int32 (-2147483648 to 2147483647)", val)
		}
		return int32(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to Int32", value)
	}
}

func (c *Client) convertToUInt32(value interface{}) (uint32, error) {
	switch v := value.(type) {
	case uint32:
		return v, nil
	case int, int8, int16, int32, int64:
		val := c.toInt64(v)
		if val < 0 || val > 4294967295 {
			return 0, fmt.Errorf("value %d out of range for UInt32 (0 to 4294967295)", val)
		}
		return uint32(val), nil
	case uint, uint8, uint16, uint64:
		val := c.toUInt64(v)
		if val > 4294967295 {
			return 0, fmt.Errorf("value %d out of range for UInt32 (0 to 4294967295)", val)
		}
		return uint32(val), nil
	case float32, float64:
		val := int64(c.toFloat64(v))
		if val < 0 || val > 4294967295 {
			return 0, fmt.Errorf("value %d out of range for UInt32 (0 to 4294967295)", val)
		}
		return uint32(val), nil
	case string:
		val, err := c.parseStringToInt64(v)
		if err != nil {
			return 0, err
		}
		if val < 0 || val > 4294967295 {
			return 0, fmt.Errorf("value %d out of range for UInt32 (0 to 4294967295)", val)
		}
		return uint32(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to UInt32", value)
	}
}

func (c *Client) convertToInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int, int8, int16, int32:
		return c.toInt64(v), nil
	case uint, uint8, uint16, uint32, uint64:
		val := c.toUInt64(v)
		if val > 9223372036854775807 {
			return 0, fmt.Errorf("value %d out of range for Int64", val)
		}
		return int64(val), nil
	case float32, float64:
		return int64(c.toFloat64(v)), nil
	case string:
		return c.parseStringToInt64(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to Int64", value)
	}
}

func (c *Client) convertToUInt64(value interface{}) (uint64, error) {
	switch v := value.(type) {
	case uint64:
		return v, nil
	case int, int8, int16, int32, int64:
		val := c.toInt64(v)
		if val < 0 {
			return 0, fmt.Errorf("value %d out of range for UInt64 (must be non-negative)", val)
		}
		return uint64(val), nil
	case uint, uint8, uint16, uint32:
		return c.toUInt64(v), nil
	case float32, float64:
		val := c.toFloat64(v)
		if val < 0 {
			return 0, fmt.Errorf("value %f out of range for UInt64 (must be non-negative)", val)
		}
		return uint64(val), nil
	case string:
		val, err := c.parseStringToUInt64(v)
		if err != nil {
			return 0, err
		}
		return val, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to UInt64", value)
	}
}

func (c *Client) convertToFloat(value interface{}) (float32, error) {
	switch v := value.(type) {
	case float32:
		return v, nil
	case float64:
		return float32(v), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return float32(c.toFloat64(v)), nil
	case string:
		val, err := c.parseStringToFloat64(v)
		if err != nil {
			return 0, err
		}
		return float32(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to Float", value)
	}
}

func (c *Client) convertToDouble(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return c.toFloat64(v), nil
	case string:
		return c.parseStringToFloat64(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to Double", value)
	}
}

func (c *Client) convertToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", c.toInt64(v)), nil
	case float32, float64:
		return fmt.Sprintf("%g", c.toFloat64(v)), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func (c *Client) convertToDateTime(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		// Try parsing common date formats
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unable to parse DateTime string: %s", v)
	case int, int64:
		// Unix timestamp
		return time.Unix(c.toInt64(v), 0), nil
	case float64:
		// Unix timestamp with fractional seconds
		sec := int64(v)
		nsec := int64((v - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	default:
		return time.Time{}, fmt.Errorf("cannot convert %T to DateTime", value)
	}
}

// Helper functions for type conversion
func (c *Client) toInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func (c *Client) toUInt64(value interface{}) uint64 {
	switch v := value.(type) {
	case int:
		return uint64(v)
	case int8:
		return uint64(v)
	case int16:
		return uint64(v)
	case int32:
		return uint64(v)
	case int64:
		return uint64(v)
	case uint:
		return uint64(v)
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case float64:
		return uint64(v)
	default:
		return 0
	}
}

func (c *Client) toFloat64(value interface{}) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

func (c *Client) parseStringToInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("invalid integer string: %q", s)
	}
	result, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer string: %q: %w", s, err)
	}
	return result, nil
}

func (c *Client) parseStringToUInt64(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("invalid unsigned integer string: %q", s)
	}
	result, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid unsigned integer string: %q: %w", s, err)
	}
	return result, nil
}

func (c *Client) parseStringToFloat64(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("invalid float string: %q", s)
	}
	result, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid float string: %q: %w", s, err)
	}
	return result, nil
}
