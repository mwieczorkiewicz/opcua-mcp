package opcua

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

// Client wraps the OPC-UA client with additional functionality
type Client struct {
	client    *opcua.Client
	config    *config.OPCUAConfig
	connected bool
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

	c.client = client
	c.connected = true
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

// Disconnect closes the connection to the OPC-UA server
func (c *Client) Disconnect(ctx context.Context) error {
	if c.client != nil && c.connected {
		err := c.client.Close(ctx)
		c.connected = false
		return err
	}
	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	return c.connected && c.client != nil
}

// SetConnectedForTesting sets the connected flag for testing purposes
func (c *Client) SetConnectedForTesting(connected bool) {
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
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read from OPC-UA server: %w", err)
	}

	// Check for errors in response
	for i, result := range resp.Results {
		if result.Status != ua.StatusOK {
			return nil, fmt.Errorf("read failed for node %s: %s", nodeIDs[i], result.Status)
		}
	}

	return resp.Results, nil
}

// Write writes values to the specified node IDs with type validation
func (c *Client) Write(ctx context.Context, nodeID string, value interface{}) error {
	if !c.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return fmt.Errorf("invalid node ID %s: %w", nodeID, err)
	}

	// Validate value against node's data type before attempting to write
	if err := c.ValidateValueForNode(ctx, nodeID, value); err != nil {
		return fmt.Errorf("type validation failed: %w", err)
	}

	// Convert value to ua.Variant
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
					Value: variant,
				},
			},
		},
	}

	// Send write request
	resp, err := c.client.Write(ctx, req)
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
	if !c.IsConnected() {
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
	resp, err := c.client.Browse(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to browse OPC-UA server: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].StatusCode != ua.StatusOK {
		return nil, fmt.Errorf("browse failed for node %s: %s", nodeID, resp.Results[0].StatusCode)
	}

	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no browse results returned")
	}

	return resp.Results[0].References, nil
}

// GetNodeClass returns the node class of the specified node
func (c *Client) GetNodeClass(ctx context.Context, nodeID string) (ua.NodeClass, error) {
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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
	nodeClass, ok := resp.Results[0].Value.Value().(ua.NodeClass)
	if !ok {
		return 0, fmt.Errorf("failed to extract node class from result")
	}

	return nodeClass, nil
}

// GetNodeDataType returns the data type information for a variable node
func (c *Client) GetNodeDataType(ctx context.Context, nodeID string) (*ua.NodeID, error) {
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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
	if !c.IsConnected() {
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
	resp, err := c.client.Read(ctx, req)
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

	// Get array dimensions
	arrayDimensions, err := c.GetNodeArrayDimensions(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get array dimensions: %w", err)
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

// ValidateValueForNode validates that a value is compatible with the node's data type
func (c *Client) ValidateValueForNode(ctx context.Context, nodeID string, value interface{}) error {
	// Get node type information
	typeInfo, err := c.GetNodeTypeInfo(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to get node type information: %w", err)
	}

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
	case 1: // Boolean
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean value, got %T", value)
		}
	case 2: // SByte
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for SByte, got %T", value)
		}
		if !c.isInRange(value, -128, 127) {
			return fmt.Errorf("SByte value out of range (-128 to 127)")
		}
	case 3: // Byte
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Byte, got %T", value)
		}
		if !c.isInRange(value, 0, 255) {
			return fmt.Errorf("Byte value out of range (0 to 255)")
		}
	case 4: // Int16
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int16, got %T", value)
		}
		if !c.isInRange(value, -32768, 32767) {
			return fmt.Errorf("Int16 value out of range (-32768 to 32767)")
		}
	case 5: // UInt16
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt16, got %T", value)
		}
		if !c.isInRange(value, 0, 65535) {
			return fmt.Errorf("UInt16 value out of range (0 to 65535)")
		}
	case 6: // Int32
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int32, got %T", value)
		}
		if !c.isInRange(value, -2147483648, 2147483647) {
			return fmt.Errorf("Int32 value out of range (-2147483648 to 2147483647)")
		}
	case 7: // UInt32
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt32, got %T", value)
		}
		if !c.isInRange(value, 0, 4294967295) {
			return fmt.Errorf("UInt32 value out of range (0 to 4294967295)")
		}
	case 8: // Int64
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for Int64, got %T", value)
		}
	case 9: // UInt64
		if !c.isIntegerType(value) {
			return fmt.Errorf("expected integer value for UInt64, got %T", value)
		}
		if !c.isNonNegative(value) {
			return fmt.Errorf("UInt64 value must be non-negative")
		}
	case 10: // Float
		if !c.isFloatType(value) {
			return fmt.Errorf("expected float value, got %T", value)
		}
	case 11: // Double
		if !c.isFloatType(value) {
			return fmt.Errorf("expected float value, got %T", value)
		}
	case 12: // String
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value, got %T", value)
		}
	case 13: // DateTime
		if !c.isDateTimeType(value) {
			return fmt.Errorf("expected DateTime value (string or number), got %T", value)
		}
	case 14: // Guid
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string value for Guid, got %T", value)
		}
	case 15: // ByteString
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
