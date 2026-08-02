package opcua

import (
	"context"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

// valueTypeBrowseStore is the narrow slice of store.Store's surface
// CachingClient needs, mirroring the opcuaClient/subscriptionStore
// interface-seam pattern already used in this package.
type valueTypeBrowseStore interface {
	GetValue(ctx context.Context, nodeID string) (store.ValueEntry, bool, error)
	PutValue(ctx context.Context, nodeID string, entry store.ValueEntry) error
	DeleteValue(ctx context.Context, nodeID string) error
	GetTypeInfo(ctx context.Context, nodeID string) (store.TypeInfoEntry, bool, error)
	PutTypeInfo(ctx context.Context, nodeID string, entry store.TypeInfoEntry) error
	GetBrowse(ctx context.Context, parentNodeID string) (store.BrowseEntry, bool, error)
	PutBrowse(ctx context.Context, parentNodeID string, entry store.BrowseEntry) error
}

// ReadResult is CachingClient.Read's per-node result, extending the raw
// value/status/timestamps Client.Read returns with cache provenance
// (requirements.md FR-3.3).
type ReadResult struct {
	NodeID          string
	Value           interface{}
	Status          string
	SourceTimestamp time.Time
	ServerTimestamp time.Time
	Source          string // "live" | "cache" | "subscription"
	CachedAt        time.Time
}

// CachingClient is a read-through caching decorator wrapping *Client
// (application-design.md's components.md). Client itself stays a pure
// OPC-UA protocol wrapper; CachingClient adds the cache-or-live decision
// on top, per requirements.md FR-3.
type CachingClient struct {
	client *Client
	cache  valueTypeBrowseStore
	cfg    *config.SearchConfig

	typeInfoTTL time.Duration
	browseTTL   time.Duration
}

// NewCachingClient constructs a CachingClient. typeInfoTTL/browseTTL come
// from config.StoreConfig (STORE_TYPEINFO_TTL/STORE_BROWSE_TTL) - passed
// explicitly rather than the whole StoreConfig struct, since those are the
// only two fields this component needs (functional-design/domain-entities.md).
func NewCachingClient(client *Client, cache valueTypeBrowseStore, searchCfg *config.SearchConfig, typeInfoTTL, browseTTL time.Duration) *CachingClient {
	return &CachingClient{
		client:      client,
		cache:       cache,
		cfg:         searchCfg,
		typeInfoTTL: typeInfoTTL,
		browseTTL:   browseTTL,
	}
}

// Read serves each requested node from the values-bucket cache when
// possible, falling back to a live batched Client.Read otherwise
// (business-rules.md BR-1/BR-2).
func (c *CachingClient) Read(ctx context.Context, nodeIDs []string, maxAgeMs int) ([]ReadResult, error) {
	if !c.cfg.EnableCache {
		return c.readLive(ctx, nodeIDs)
	}

	maxAge := time.Duration(maxAgeMs) * time.Millisecond
	results := make([]ReadResult, len(nodeIDs))
	liveIdx := make([]int, 0, len(nodeIDs))
	liveIDs := make([]string, 0, len(nodeIDs))

	for i, nodeID := range nodeIDs {
		entry, ok, err := c.cache.GetValue(ctx, nodeID)
		if err != nil {
			logger.Warn("CachingClient: values cache read failed, falling back to live", "node_id", nodeID, "error", err)
			ok = false
		}
		if !ok {
			liveIdx = append(liveIdx, i)
			liveIDs = append(liveIDs, nodeID)
			continue
		}

		switch entry.Source {
		case "subscription":
			results[i] = valueEntryToReadResult(nodeID, entry, "subscription")
		case "live":
			if time.Since(entry.ReceivedAt) <= maxAge {
				results[i] = valueEntryToReadResult(nodeID, entry, "cache")
			} else {
				liveIdx = append(liveIdx, i)
				liveIDs = append(liveIDs, nodeID)
			}
		default:
			// Unknown/empty provenance - treat like a miss rather than
			// trusting an entry we can't classify.
			liveIdx = append(liveIdx, i)
			liveIDs = append(liveIDs, nodeID)
		}
	}

	if len(liveIDs) == 0 {
		return results, nil
	}

	liveResults, err := c.readLive(ctx, liveIDs)
	if err != nil {
		return nil, err
	}
	for j, idx := range liveIdx {
		results[idx] = liveResults[j]
	}
	return results, nil
}

// readLive performs a live batched Client.Read and opportunistically
// caches each result with Source: "live".
func (c *CachingClient) readLive(ctx context.Context, nodeIDs []string) ([]ReadResult, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	values, err := c.client.Read(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	results := make([]ReadResult, len(nodeIDs))
	for i, v := range values {
		result := ReadResult{
			NodeID:          nodeIDs[i],
			Value:           v.Value.Value(),
			Status:          v.Status.Error(),
			SourceTimestamp: v.SourceTimestamp,
			ServerTimestamp: v.ServerTimestamp,
			Source:          "live",
		}
		results[i] = result

		if c.cfg.EnableCache {
			entry := store.ValueEntry{
				Value:           result.Value,
				Status:          result.Status,
				SourceTimestamp: result.SourceTimestamp,
				ServerTimestamp: result.ServerTimestamp,
				ReceivedAt:      now,
				Source:          "live",
			}
			if putErr := c.cache.PutValue(ctx, nodeIDs[i], entry); putErr != nil {
				logger.Warn("CachingClient: opportunistic values cache write failed", "node_id", nodeIDs[i], "error", putErr)
			}
		}
	}
	return results, nil
}

func valueEntryToReadResult(nodeID string, entry store.ValueEntry, source string) ReadResult {
	return ReadResult{
		NodeID:          nodeID,
		Value:           entry.Value,
		Status:          entry.Status,
		SourceTimestamp: entry.SourceTimestamp,
		ServerTimestamp: entry.ServerTimestamp,
		Source:          source,
		CachedAt:        entry.ReceivedAt,
	}
}

// Browse checks the browse-bucket cache first, keyed by nodeID, within
// browseTTL (business-rules.md BR-3).
func (c *CachingClient) Browse(ctx context.Context, nodeID string) ([]store.BrowseReference, error) {
	if c.cfg.EnableCache {
		entry, ok, err := c.cache.GetBrowse(ctx, nodeID)
		if err != nil {
			logger.Warn("CachingClient: browse cache read failed, falling back to live", "node_id", nodeID, "error", err)
		} else if ok && time.Since(entry.CachedAt) <= c.browseTTL {
			return entry.References, nil
		}
	}

	refs, err := c.client.Browse(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	converted := make([]store.BrowseReference, len(refs))
	for i, ref := range refs {
		converted[i] = store.BrowseReference{
			NodeID:         ref.NodeID.String(),
			BrowseName:     ref.BrowseName.Name,
			DisplayName:    ref.DisplayName.Text,
			NodeClass:      ref.NodeClass.String(),
			TypeDefinition: ref.TypeDefinition.String(),
		}
	}

	if c.cfg.EnableCache {
		entry := store.BrowseEntry{References: converted, CachedAt: time.Now()}
		if err := c.cache.PutBrowse(ctx, nodeID, entry); err != nil {
			logger.Warn("CachingClient: browse cache write failed", "node_id", nodeID, "error", err)
		}
	}

	return converted, nil
}

// GetNodeTypeInfo checks the typeinfo-bucket cache first, within
// typeInfoTTL (business-rules.md BR-4).
func (c *CachingClient) GetNodeTypeInfo(ctx context.Context, nodeID string) (*NodeTypeInfo, error) {
	if c.cfg.EnableCache {
		entry, ok, err := c.cache.GetTypeInfo(ctx, nodeID)
		if err != nil {
			logger.Warn("CachingClient: typeinfo cache read failed, falling back to live", "node_id", nodeID, "error", err)
		} else if ok && time.Since(entry.CachedAt) <= c.typeInfoTTL {
			return typeInfoEntryToNodeTypeInfo(entry), nil
		}
	}

	typeInfo, err := c.client.GetNodeTypeInfo(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	if c.cfg.EnableCache {
		entry := store.TypeInfoEntry{
			DataTypeID:      typeInfo.DataType.IntID(),
			ValueRank:       typeInfo.ValueRank,
			ArrayDimensions: typeInfo.ArrayDimensions,
			AccessLevel:     typeInfo.AccessLevel,
			UserAccessLevel: typeInfo.UserAccessLevel,
			CachedAt:        time.Now(),
		}
		if err := c.cache.PutTypeInfo(ctx, nodeID, entry); err != nil {
			logger.Warn("CachingClient: typeinfo cache write failed", "node_id", nodeID, "error", err)
		}
	}

	return typeInfo, nil
}

// typeInfoEntryToNodeTypeInfo reconstructs a *NodeTypeInfo from a cached
// TypeInfoEntry, assuming namespace 0 for DataType - matching this
// codebase's existing precedent (convertValueToOPCUAType/
// validateScalarValue already operate purely on DataType.IntID(), never
// checking namespace).
func typeInfoEntryToNodeTypeInfo(entry store.TypeInfoEntry) *NodeTypeInfo {
	isWritable := (entry.AccessLevel & writeAccessBit) != 0
	isUserWritable := (entry.UserAccessLevel & writeAccessBit) != 0
	return &NodeTypeInfo{
		DataType:        ua.NewNumericNodeID(0, entry.DataTypeID),
		ValueRank:       entry.ValueRank,
		ArrayDimensions: entry.ArrayDimensions,
		IsArray:         entry.ValueRank > 0,
		IsScalar:        entry.ValueRank == -1 || entry.ValueRank == 0,
		AccessLevel:     entry.AccessLevel,
		UserAccessLevel: entry.UserAccessLevel,
		IsWritable:      isWritable,
		IsUserWritable:  isUserWritable,
	}
}

// Write delegates to Client.Write, then on success invalidates the
// node's cached value unless it's currently subscribed (business-rules.md
// BR-5) - the next subscription push naturally corrects a subscribed
// entry, so deleting it would just recreate a momentary gap.
func (c *CachingClient) Write(ctx context.Context, nodeID string, value interface{}) error {
	if err := c.client.Write(ctx, nodeID, value); err != nil {
		return err
	}

	if !c.cfg.EnableCache {
		return nil
	}

	entry, ok, err := c.cache.GetValue(ctx, nodeID)
	if err != nil {
		logger.Warn("CachingClient: values cache read failed during write invalidation", "node_id", nodeID, "error", err)
		return nil
	}
	if !ok || entry.Source == "subscription" {
		return nil
	}

	if err := c.cache.DeleteValue(ctx, nodeID); err != nil {
		logger.Warn("CachingClient: values cache invalidation failed", "node_id", nodeID, "error", err)
	}
	return nil
}
