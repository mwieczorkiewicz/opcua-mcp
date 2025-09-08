package opcua

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
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

// Write writes values to the specified node IDs
func (c *Client) Write(ctx context.Context, nodeID string, value interface{}) error {
	if !c.IsConnected() {
		return fmt.Errorf("client is not connected")
	}

	// Parse node ID
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return fmt.Errorf("invalid node ID %s: %w", nodeID, err)
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

// GetServerInfo returns information about the OPC-UA server
func (c *Client) GetServerInfo(ctx context.Context) (*ua.ServerStatusDataType, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("client is not connected")
	}

	// Read server status from the server node
	req := &ua.ReadRequest{
		MaxAge:             0,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
		NodesToRead: []*ua.ReadValueID{
			{
				NodeID:      ua.NewNumericNodeID(0, 2253), // Server
				AttributeID: ua.AttributeIDValue,
			},
		},
	}

	// Send read request
	resp, err := c.client.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read server status: %w", err)
	}

	// Check for errors in response
	if len(resp.Results) > 0 && resp.Results[0].Status != ua.StatusOK {
		return nil, fmt.Errorf("read server status failed: %s", resp.Results[0].Status)
	}

	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("no results returned")
	}

	// Extract server status from result
	serverStatus, ok := resp.Results[0].Value.Value().(*ua.ServerStatusDataType)
	if !ok {
		return nil, fmt.Errorf("failed to extract server status from result")
	}

	return serverStatus, nil
}
