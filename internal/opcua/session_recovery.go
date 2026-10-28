package opcua

import (
	"context"
	"errors"
	"fmt"

	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// staleSessionStatuses are the service results that mean the server no
// longer recognizes this client's session (or the client has no usable
// channel to it), so the request was rejected without being executed and a
// fresh connection is needed.
var staleSessionStatuses = []ua.StatusCode{
	ua.StatusBadSessionIDInvalid,
	ua.StatusBadServerNotConnected,
}

// isStaleSessionError reports whether err's chain contains one of
// staleSessionStatuses.
func isStaleSessionError(err error) bool {
	for _, status := range staleSessionStatuses {
		if errors.Is(err, status) {
			return true
		}
	}
	return false
}

// SetReconnectHandler registers fn to run after session recovery replaces a
// live connection, e.g. to rebuild subscriptions that died with the old
// one. fn receives a context detached from the triggering request's
// cancellation. Pass nil to unregister.
func (c *Client) SetReconnectHandler(fn func(context.Context)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReconnect = fn
}

// recoverConnection replaces the connection identified by gen after it
// failed with a stale session. Reconnects are serialized: if another caller
// already replaced that connection, this returns nil without reconnecting
// again. A connection the user disconnected is never brought back.
func (c *Client) recoverConnection(ctx context.Context, gen uint64, op string) error {
	c.connectMu.Lock()
	if c.liveGen() != gen {
		c.connectMu.Unlock()
		return nil
	}
	if !c.IsConnected() {
		c.connectMu.Unlock()
		return fmt.Errorf("client is not connected")
	}

	logger.Warn("OPC-UA session is no longer valid, reconnecting", "operation", op)
	err := c.connectLocked(ctx)
	c.mu.RLock()
	handler := c.onReconnect
	c.mu.RUnlock()
	c.connectMu.Unlock()

	if err != nil {
		return fmt.Errorf("reconnect: %w", err)
	}
	logger.Info("OPC-UA session re-established", "operation", op)
	if handler != nil {
		handler(context.WithoutCancel(ctx))
	}
	return nil
}

// withSessionRecovery runs a read-only operation and, if it fails with a
// stale session, reconnects and runs it exactly once more. Only read-only
// operations may use it; Write reconnects without retrying (see sendWrite).
func withSessionRecovery[T any](ctx context.Context, c *Client, op string, fn func() (T, error)) (T, error) {
	gen := c.liveGen()
	result, err := fn()
	if err == nil || !isStaleSessionError(err) {
		return result, err
	}
	if rerr := c.recoverConnection(ctx, gen, op); rerr != nil {
		return result, fmt.Errorf("%w (session recovery failed: %v)", err, rerr)
	}
	return fn()
}

// Read reads values from the specified node IDs, recovering once from a
// stale session.
func (c *Client) Read(ctx context.Context, nodeIDs []string) ([]*ua.DataValue, error) {
	return withSessionRecovery(ctx, c, "read", func() ([]*ua.DataValue, error) { return c.read(ctx, nodeIDs) })
}

// Browse browses the node hierarchy starting from the specified node,
// recovering once from a stale session.
func (c *Client) Browse(ctx context.Context, nodeID string) ([]*ua.ReferenceDescription, error) {
	return withSessionRecovery(ctx, c, "browse", func() ([]*ua.ReferenceDescription, error) { return c.browse(ctx, nodeID) })
}

// GetNodeClass returns the node class of the specified node, recovering once
// from a stale session.
func (c *Client) GetNodeClass(ctx context.Context, nodeID string) (ua.NodeClass, error) {
	return withSessionRecovery(ctx, c, "read node class", func() (ua.NodeClass, error) { return c.getNodeClass(ctx, nodeID) })
}

// GetNodeDataType returns the data type of a variable node, recovering once
// from a stale session.
func (c *Client) GetNodeDataType(ctx context.Context, nodeID string) (*ua.NodeID, error) {
	return withSessionRecovery(ctx, c, "read data type", func() (*ua.NodeID, error) { return c.getNodeDataType(ctx, nodeID) })
}

// GetNodeValueRank returns the value rank of a variable node, recovering
// once from a stale session.
func (c *Client) GetNodeValueRank(ctx context.Context, nodeID string) (int32, error) {
	return withSessionRecovery(ctx, c, "read value rank", func() (int32, error) { return c.getNodeValueRank(ctx, nodeID) })
}

// GetNodeArrayDimensions returns the array dimensions of a variable node,
// recovering once from a stale session.
func (c *Client) GetNodeArrayDimensions(ctx context.Context, nodeID string) ([]uint32, error) {
	return withSessionRecovery(ctx, c, "read array dimensions", func() ([]uint32, error) { return c.getNodeArrayDimensions(ctx, nodeID) })
}

// GetNodeAccessLevel returns the access level of a variable node,
// recovering once from a stale session.
func (c *Client) GetNodeAccessLevel(ctx context.Context, nodeID string) (byte, error) {
	return withSessionRecovery(ctx, c, "read access level", func() (byte, error) { return c.getNodeAccessLevel(ctx, nodeID) })
}

// GetNodeUserAccessLevel returns the user access level of a variable node,
// recovering once from a stale session.
func (c *Client) GetNodeUserAccessLevel(ctx context.Context, nodeID string) (byte, error) {
	return withSessionRecovery(ctx, c, "read user access level", func() (byte, error) { return c.getNodeUserAccessLevel(ctx, nodeID) })
}

// sendWrite sends a write request on the current connection. A stale
// session reconnects but does not retry: the server rejected the request
// without applying it, and the caller decides whether to write again.
func (c *Client) sendWrite(ctx context.Context, req *ua.WriteRequest, nodeID string) error {
	gen := c.liveGen()
	client, connected := c.snapshot()
	if !connected || client == nil {
		return fmt.Errorf("client is not connected")
	}

	resp, err := client.Write(ctx, req)
	if err != nil {
		if !isStaleSessionError(err) {
			return fmt.Errorf("failed to write to OPC-UA server: %w", err)
		}
		if rerr := c.recoverConnection(ctx, gen, "write"); rerr != nil {
			return fmt.Errorf("write to node %s was not applied: the OPC-UA session was no longer valid and reconnecting failed (%v): %w", nodeID, rerr, err)
		}
		return fmt.Errorf("write to node %s was not applied: the OPC-UA session was no longer valid and has been re-established, retry the write: %w", nodeID, err)
	}

	if len(resp.Results) > 0 && resp.Results[0] != ua.StatusOK {
		return fmt.Errorf("write failed for node %s: %s", nodeID, resp.Results[0])
	}
	return nil
}
