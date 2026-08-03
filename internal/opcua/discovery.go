package opcua

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry"
)

// NodeInfo represents information about an OPC-UA node
type NodeInfo struct {
	NodeID         string    `json:"node_id"`
	BrowseName     string    `json:"browse_name"`
	DisplayName    string    `json:"display_name"`
	NodeClass      string    `json:"node_class"`
	TypeDefinition string    `json:"type_definition"`
	ParentNode     string    `json:"parent_node"`
	Depth          int       `json:"depth"`
	LastUpdated    time.Time `json:"last_updated"`

	// SeenInGen is the discovery walk generation this node was last observed
	// in; entries whose SeenInGen falls behind the current generation after
	// a walk completes no longer exist on the live server and are swept
	// (findings.md H6/H7/H8). Internal bookkeeping only - not part of the
	// tool-facing JSON shape or the Bleve document (both key off the json
	// tag), so a stale walk generation never leaks past this package.
	SeenInGen uint64 `json:"-"`
}

// DiscoveryService handles node discovery and search functionality
type DiscoveryService struct {
	client     *Client
	config     *config.SearchConfig
	index      bleve.Index
	nodeCache  map[string]*NodeInfo
	cacheMutex sync.RWMutex
	stopChan   chan struct{}
	wg         sync.WaitGroup

	// discoveryMu serializes discoverNodes so the ticker-driven worker and an
	// explicit opcua_force_discovery call never walk concurrently - both
	// would otherwise race on walkGen and the mark-and-sweep bookkeeping.
	discoveryMu sync.Mutex
	// walkGen is the current discovery generation, incremented once per walk
	// under discoveryMu.
	walkGen uint64

	telemetry telemetry.Telemetry
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(client *Client, cfg *config.SearchConfig) (*DiscoveryService, error) {
	var index bleve.Index
	var err error

	if cfg.EnableSearch {
		// Try to open existing index
		index, err = bleve.Open(cfg.SearchIndexPath)
		if err != nil {
			// Create new index if it doesn't exist
			mapping := bleve.NewIndexMapping()

			// Configure text field mapping for browse names and display names
			textFieldMapping := bleve.NewTextFieldMapping()
			textFieldMapping.Analyzer = "en"

			// Configure keyword field mapping for exact matches
			keywordFieldMapping := bleve.NewKeywordFieldMapping()

			// Create document mapping
			documentMapping := bleve.NewDocumentMapping()
			documentMapping.AddFieldMappingsAt("browse_name", textFieldMapping, keywordFieldMapping)
			documentMapping.AddFieldMappingsAt("display_name", textFieldMapping, keywordFieldMapping)
			documentMapping.AddFieldMappingsAt("node_id", keywordFieldMapping)
			documentMapping.AddFieldMappingsAt("node_class", keywordFieldMapping)
			documentMapping.AddFieldMappingsAt("type_definition", keywordFieldMapping)
			documentMapping.AddFieldMappingsAt("parent_node", keywordFieldMapping)

			mapping.AddDocumentMapping("node", documentMapping)

			index, err = bleve.New(cfg.SearchIndexPath, mapping)
			if err != nil {
				return nil, fmt.Errorf("failed to create search index: %w", err)
			}
		}
	}

	return &DiscoveryService{
		client:    client,
		config:    cfg,
		index:     index,
		nodeCache: make(map[string]*NodeInfo),
		stopChan:  make(chan struct{}),
		telemetry: telemetry.New(telemetry.Config{}),
	}, nil
}

// SetTelemetry wires up a real Telemetry instance, replacing the no-op
// default NewDiscoveryService constructs.
func (ds *DiscoveryService) SetTelemetry(tel telemetry.Telemetry) {
	ds.telemetry = tel
}

// Start starts the discovery service
func (ds *DiscoveryService) Start(ctx context.Context) error {
	if !ds.config.EnableDiscovery {
		logger.Info("Node discovery is disabled")
		return nil
	}

	logger.Info("Starting node discovery service", "interval", ds.config.DiscoveryInterval)

	// Start background discovery worker
	ds.wg.Add(1)
	go ds.discoveryWorker(ctx)

	return nil
}

// Stop stops the discovery service
func (ds *DiscoveryService) Stop() error {
	if !ds.config.EnableDiscovery {
		return nil
	}

	logger.Info("Stopping node discovery service")
	close(ds.stopChan)
	ds.wg.Wait()

	// Close search index
	if ds.index != nil {
		return ds.index.Close()
	}

	return nil
}

// discoveryWorker runs the background discovery process
func (ds *DiscoveryService) discoveryWorker(ctx context.Context) {
	defer ds.wg.Done()

	ticker := time.NewTicker(ds.config.DiscoveryInterval)
	defer ticker.Stop()

	// Run initial discovery
	if err := ds.discoverNodes(ctx); err != nil {
		logger.Error("Initial node discovery failed", "error", err)
	}

	for {
		select {
		case <-ds.stopChan:
			return
		case <-ticker.C:
			if err := ds.discoverNodes(ctx); err != nil {
				logger.Error("Node discovery failed", "error", err)
			}
		}
	}
}

// discoverNodes performs a full node discovery starting from the root node.
// It serializes against any other concurrent discoverNodes call (the
// ticker-driven worker vs. an explicit opcua_force_discovery) so only one
// walk - and one walkGen - is ever in flight at a time.
func (ds *DiscoveryService) discoverNodes(ctx context.Context) error {
	if !ds.client.IsConnected() {
		return fmt.Errorf("OPC-UA client is not connected")
	}

	ds.discoveryMu.Lock()
	defer ds.discoveryMu.Unlock()

	ds.walkGen++
	currentGen := ds.walkGen

	logger.Info("Starting node discovery", "root_node", ds.config.DiscoveryRootNode, "max_depth", ds.config.MaxDiscoveryDepth, "max_nodes_per_browse", ds.config.MaxNodesPerBrowse, "generation", currentGen)

	// Walk the address space, upserting each node in place (under
	// cacheMutex.Lock() per node) instead of wiping nodeCache first. A
	// concurrent reader (GetNodeInfo, SearchNodes, ...) therefore never
	// observes an empty-then-partially-rebuilt cache mid-walk (findings.md
	// H7). visited tracks nodes whose own children have already been
	// browsed in this generation, so a node reachable via more than one
	// hierarchical reference - OPC-UA address spaces are graphs, not trees -
	// isn't re-browsed for each incoming reference (findings.md H6).
	visited := make(map[string]bool)
	discovered, err := ds.discoverNodeRecursive(ctx, ds.config.DiscoveryRootNode, "", 0, currentGen, visited)
	if err != nil {
		return fmt.Errorf("failed to discover nodes: %w", err)
	}

	// Sweep: anything not touched in this generation no longer exists on the
	// live server (or is unreachable from the root within MaxDiscoveryDepth)
	// and must be removed from both the in-memory cache and the Bleve index
	// (findings.md H8 - the index previously never deleted anything).
	ds.cacheMutex.Lock()
	var swept []string
	for id, info := range ds.nodeCache {
		if info.SeenInGen != currentGen {
			swept = append(swept, id)
			delete(ds.nodeCache, id)
		}
	}
	cacheSize := len(ds.nodeCache)
	ds.cacheMutex.Unlock()

	ds.telemetry.SetDiscoveryStats(cacheSize)

	logger.Info("Node discovery completed", "discovered_nodes", discovered, "cache_size", cacheSize, "swept_nodes", len(swept), "generation", currentGen)

	// Update search index if enabled
	if ds.config.EnableSearch && ds.index != nil {
		if err := ds.updateSearchIndex(swept); err != nil {
			logger.Error("Failed to update search index", "error", err)
			return fmt.Errorf("failed to update search index: %w", err)
		}
		logger.Info("Search index updated successfully", "indexed_nodes", cacheSize, "deleted_nodes", len(swept))
	} else {
		logger.Warn("Search indexing is disabled or index is nil")
	}

	return nil
}

// discoverNodeRecursive recursively discovers nodes starting from a given
// node. visited is scoped to a single discoverNodes call (discoveryMu keeps
// only one walk running at a time, so no extra locking is needed for it) and
// prevents re-browsing a node whose children were already walked earlier in
// this same generation.
func (ds *DiscoveryService) discoverNodeRecursive(ctx context.Context, nodeID, parentNode string, depth int, currentGen uint64, visited map[string]bool) (int, error) {
	if depth > ds.config.MaxDiscoveryDepth {
		logger.Debug("Max discovery depth reached", "node_id", nodeID, "depth", depth, "max_depth", ds.config.MaxDiscoveryDepth)
		return 0, nil
	}

	if visited[nodeID] {
		logger.Debug("Node already walked this generation, skipping re-browse", "node_id", nodeID, "depth", depth)
		return 0, nil
	}
	visited[nodeID] = true

	// Browse the current node
	references, err := ds.client.Browse(ctx, nodeID)
	if err != nil {
		logger.Warn("Failed to browse node", "node_id", nodeID, "depth", depth, "error", err)
		// For critical Server nodes, try again with a longer timeout
		if strings.HasPrefix(nodeID, "i=22") || strings.HasPrefix(nodeID, "i=17") {
			logger.Info("Retrying browse for critical Server node", "node_id", nodeID)
			time.Sleep(100 * time.Millisecond) // Brief delay before retry
			references, err = ds.client.Browse(ctx, nodeID)
			if err != nil {
				logger.Error("Failed to browse critical Server node on retry", "node_id", nodeID, "error", err)
				return 0, nil
			}
		} else {
			return 0, nil // Continue with other nodes
		}
	}

	if len(references) == 0 {
		logger.Debug("No references found for node", "node_id", nodeID, "depth", depth)
		return 0, nil
	}

	logger.Debug("Browsing node", "node_id", nodeID, "depth", depth, "references_count", len(references))

	discovered := 0
	directChildren := 0 // direct children processed at this level only (findings.md H10e)
	for i, ref := range references {
		if directChildren >= ds.config.MaxNodesPerBrowse {
			logger.Warn("Max nodes per browse limit reached", "node_id", nodeID, "limit", ds.config.MaxNodesPerBrowse, "processed", i)
			break
		}

		// Create node info
		nodeInfo := &NodeInfo{
			NodeID:         ref.NodeID.String(),
			BrowseName:     ref.BrowseName.Name,
			DisplayName:    ref.DisplayName.Text,
			NodeClass:      ref.NodeClass.String(),
			TypeDefinition: ref.TypeDefinition.String(),
			ParentNode:     parentNode,
			Depth:          depth,
			LastUpdated:    time.Now(),
			SeenInGen:      currentGen,
		}

		// Upsert into cache (a node reachable via multiple parents keeps
		// whichever parent/depth it was most recently discovered under in
		// this walk - unchanged from prior behavior).
		ds.cacheMutex.Lock()
		ds.nodeCache[nodeInfo.NodeID] = nodeInfo
		ds.cacheMutex.Unlock()

		directChildren++
		discovered++
		logger.Debug("Discovered node", "node_id", nodeInfo.NodeID, "browse_name", nodeInfo.BrowseName, "depth", depth)

		// Recursively discover child nodes
		childDiscovered, err := ds.discoverNodeRecursive(ctx, nodeInfo.NodeID, nodeInfo.NodeID, depth+1, currentGen, visited)
		if err != nil {
			logger.Warn("Failed to discover child nodes", "node_id", nodeInfo.NodeID, "browse_name", nodeInfo.BrowseName, "error", err)
		} else {
			logger.Debug("Discovered child nodes", "node_id", nodeInfo.NodeID, "browse_name", nodeInfo.BrowseName, "child_count", childDiscovered)
		}
		discovered += childDiscovered
	}

	logger.Debug("Completed browsing node", "node_id", nodeID, "depth", depth, "total_discovered", discovered)
	return discovered, nil
}

// updateSearchIndex updates the Bleve search index with the current node
// cache and deletes any swept (no-longer-present) node IDs from the same
// batch (findings.md H8).
func (ds *DiscoveryService) updateSearchIndex(swept []string) error {
	if ds.index == nil {
		return fmt.Errorf("search index is nil")
	}

	// Create batch for efficient indexing
	batch := ds.index.NewBatch()

	ds.cacheMutex.RLock()
	nodeCount := len(ds.nodeCache)
	indexedCount := 0
	failedCount := 0

	for nodeID, nodeInfo := range ds.nodeCache {
		if err := batch.Index(nodeID, nodeInfo); err != nil {
			logger.Warn("Failed to add node to index batch", "node_id", nodeID, "browse_name", nodeInfo.BrowseName, "error", err)
			failedCount++
		} else {
			indexedCount++
		}
	}
	ds.cacheMutex.RUnlock()

	for _, id := range swept {
		batch.Delete(id)
	}

	logger.Info("Preparing to update search index", "total_nodes", nodeCount, "batch_size", indexedCount, "failed_batch_adds", failedCount, "deleted_nodes", len(swept))

	// Execute batch
	if err := ds.index.Batch(batch); err != nil {
		return fmt.Errorf("failed to execute search index batch: %w", err)
	}

	logger.Info("Updated search index successfully", "total_nodes", nodeCount, "indexed_nodes", indexedCount, "failed_nodes", failedCount, "deleted_nodes", len(swept))
	return nil
}

// GetNodeByBrowseName finds a node by its browse name
func (ds *DiscoveryService) GetNodeByBrowseName(browseName string) (*NodeInfo, error) {
	if !ds.config.EnableSearch || ds.index == nil {
		// Fallback to cache search
		return ds.getNodeByBrowseNameFromCache(browseName)
	}

	// Search using Bleve
	query := bleve.NewMatchQuery(browseName)
	query.SetField("browse_name")

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = 1

	searchResult, err := ds.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(searchResult.Hits) == 0 {
		return nil, fmt.Errorf("node with browse name '%s' not found", browseName)
	}

	// Get node info from cache
	hit := searchResult.Hits[0]
	ds.cacheMutex.RLock()
	nodeInfo, exists := ds.nodeCache[hit.ID]
	ds.cacheMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("node info not found in cache for ID %s", hit.ID)
	}

	return nodeInfo, nil
}

// FindSimilarNodes finds nodes similar to the given browse name using a tiered search strategy
func (ds *DiscoveryService) FindSimilarNodes(browseName string) ([]*NodeInfo, error) {
	if !ds.config.EnableSearch || ds.index == nil {
		return ds.findSimilarNodesFromCache(browseName)
	}

	// Tier 1: Exact match (fastest, most precise)
	if nodes, err := ds.searchExact(browseName); err == nil && len(nodes) > 0 {
		return nodes, nil
	}

	// Tier 2: Wildcard contains (fast, good for partial matches)
	if nodes, err := ds.searchWildcard(browseName); err == nil && len(nodes) > 0 {
		return nodes, nil
	}

	// Tier 3: Fuzzy search (slower, but handles typos)
	if nodes, err := ds.searchFuzzy(browseName); err == nil && len(nodes) > 0 {
		return nodes, nil
	}

	// Tier 4: Fallback to cache-based search (no index overhead)
	return ds.findSimilarNodesFromCache(browseName)
}

// searchExact performs exact term matching
func (ds *DiscoveryService) searchExact(browseName string) ([]*NodeInfo, error) {
	query := bleve.NewTermQuery(browseName)
	query.SetField("browse_name")

	request := bleve.NewSearchRequest(query)
	request.Size = ds.config.SearchMaxResults

	result, err := ds.index.Search(request)
	if err != nil {
		return nil, err
	}

	return ds.convertSearchResults(result), nil
}

// searchWildcard performs wildcard contains matching
func (ds *DiscoveryService) searchWildcard(browseName string) ([]*NodeInfo, error) {
	query := bleve.NewWildcardQuery("*" + browseName + "*")
	query.SetField("browse_name")

	request := bleve.NewSearchRequest(query)
	request.Size = ds.config.SearchMaxResults

	result, err := ds.index.Search(request)
	if err != nil {
		return nil, err
	}

	return ds.convertSearchResults(result), nil
}

// searchFuzzy performs fuzzy matching with optimal fuzziness
func (ds *DiscoveryService) searchFuzzy(browseName string) ([]*NodeInfo, error) {
	// Use fuzziness=1 for optimal balance of recall vs precision
	query := bleve.NewFuzzyQuery(browseName)
	query.SetField("browse_name")
	query.SetFuzziness(1)

	request := bleve.NewSearchRequest(query)
	request.Size = ds.config.SearchMaxResults

	result, err := ds.index.Search(request)
	if err != nil {
		return nil, err
	}

	return ds.convertSearchResults(result), nil
}

// convertSearchResults converts Bleve search results to NodeInfo slice
func (ds *DiscoveryService) convertSearchResults(result *bleve.SearchResult) []*NodeInfo {
	var nodes []*NodeInfo
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	for _, hit := range result.Hits {
		if nodeInfo, exists := ds.nodeCache[hit.ID]; exists {
			nodes = append(nodes, nodeInfo)
		}
	}

	return nodes
}

// GetNodeInfo returns node information by node ID
func (ds *DiscoveryService) GetNodeInfo(nodeID string) (*NodeInfo, error) {
	ds.cacheMutex.RLock()
	nodeInfo, exists := ds.nodeCache[nodeID]
	ds.cacheMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("node %s not found in cache", nodeID)
	}

	return nodeInfo, nil
}

// GetAllNodes returns all discovered nodes
func (ds *DiscoveryService) GetAllNodes() []*NodeInfo {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	nodes := make([]*NodeInfo, 0, len(ds.nodeCache))
	for _, nodeInfo := range ds.nodeCache {
		nodes = append(nodes, nodeInfo)
	}

	return nodes
}

// getNodeByBrowseNameFromCache searches for a node by browse name in the cache
func (ds *DiscoveryService) getNodeByBrowseNameFromCache(browseName string) (*NodeInfo, error) {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	for _, nodeInfo := range ds.nodeCache {
		if nodeInfo.BrowseName == browseName {
			return nodeInfo, nil
		}
	}

	return nil, fmt.Errorf("node with browse name '%s' not found", browseName)
}

// findSimilarNodesFromCache searches for similar nodes in the cache using tiered matching
func (ds *DiscoveryService) findSimilarNodesFromCache(browseName string) ([]*NodeInfo, error) {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	if len(ds.nodeCache) == 0 {
		return nil, nil
	}

	browseNameLower := strings.ToLower(browseName)
	var exactMatches, containsMatches, fuzzyMatches []*NodeInfo

	// Single pass through cache with tiered matching
	for _, nodeInfo := range ds.nodeCache {
		if len(nodeInfo.BrowseName) == 0 {
			continue
		}

		nodeNameLower := strings.ToLower(nodeInfo.BrowseName)

		// Tier 1: Exact matches (highest priority)
		if nodeInfo.BrowseName == browseName || nodeNameLower == browseNameLower {
			exactMatches = append(exactMatches, nodeInfo)
			continue
		}

		// Tier 2: Contains matches (good for partial matches)
		if strings.Contains(nodeNameLower, browseNameLower) {
			containsMatches = append(containsMatches, nodeInfo)
			continue
		}

		// Tier 3: Fuzzy matches (lowest priority, most expensive)
		if ds.isSimilarString(nodeNameLower, browseNameLower) {
			fuzzyMatches = append(fuzzyMatches, nodeInfo)
		}
	}

	// Return results in priority order, limiting total results
	maxResults := ds.config.SearchMaxResults
	var result []*NodeInfo

	// Add exact matches first
	for _, node := range exactMatches {
		if len(result) >= maxResults {
			break
		}
		result = append(result, node)
	}

	// Add contains matches
	for _, node := range containsMatches {
		if len(result) >= maxResults {
			break
		}
		result = append(result, node)
	}

	// Add fuzzy matches
	for _, node := range fuzzyMatches {
		if len(result) >= maxResults {
			break
		}
		result = append(result, node)
	}

	return result, nil
}

// isSimilarString performs efficient fuzzy string matching using prefix/suffix similarity
func (ds *DiscoveryService) isSimilarString(s1, s2 string) bool {
	// Early exit for very different lengths
	if len(s1) > len(s2)*2 || len(s2) > len(s1)*2 {
		return false
	}

	// Quick prefix/suffix check (most common case for similar strings)
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	// Check prefix similarity (first 3+ chars)
	if minLen >= 3 {
		prefixLen := minLen / 2
		if prefixLen > 3 {
			prefixLen = 3
		}
		if s1[:prefixLen] == s2[:prefixLen] {
			return true
		}
	}

	// Check suffix similarity (last 3+ chars)
	if minLen >= 3 {
		suffixLen := minLen / 2
		if suffixLen > 3 {
			suffixLen = 3
		}
		if s1[len(s1)-suffixLen:] == s2[len(s2)-suffixLen:] {
			return true
		}
	}

	// Fallback to character overlap for short strings
	if minLen <= 6 {
		overlap := 0
		for i := 0; i < minLen; i++ {
			if s1[i] == s2[i] {
				overlap++
			}
		}
		return float64(overlap)/float64(minLen) > 0.7
	}

	return false
}

// SearchNodes performs a comprehensive search across multiple fields
func (ds *DiscoveryService) SearchNodes(query string, nodeClass string, maxResults int) ([]*NodeInfo, error) {
	if !ds.config.EnableSearch || ds.index == nil {
		// Fallback to cache search
		return ds.searchNodesFromCache(query, nodeClass, maxResults)
	}

	// Create a boolean query for multi-field search
	boolQuery := bleve.NewBooleanQuery()

	// Add text search across browse name and display name
	if query != "" {
		browseNameQuery := bleve.NewMatchQuery(query)
		browseNameQuery.SetField("browse_name")

		displayNameQuery := bleve.NewMatchQuery(query)
		displayNameQuery.SetField("display_name")

		textQuery := bleve.NewDisjunctionQuery(browseNameQuery, displayNameQuery)
		boolQuery.AddMust(textQuery)
	}

	// Add node class filter if specified
	if nodeClass != "" {
		nodeClassQuery := bleve.NewTermQuery(nodeClass)
		nodeClassQuery.SetField("node_class")
		boolQuery.AddMust(nodeClassQuery)
	}

	// Create search request
	searchRequest := bleve.NewSearchRequest(boolQuery)
	if maxResults > 0 {
		searchRequest.Size = maxResults
	} else {
		searchRequest.Size = ds.config.SearchMaxResults
	}
	searchRequest.SortBy([]string{"-_score"}) // Sort by relevance

	searchResult, err := ds.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert search results to node info
	var nodes []*NodeInfo
	ds.cacheMutex.RLock()
	for _, hit := range searchResult.Hits {
		if hit.Score >= ds.config.SearchMinScore {
			if nodeInfo, exists := ds.nodeCache[hit.ID]; exists {
				nodes = append(nodes, nodeInfo)
			}
		}
	}
	ds.cacheMutex.RUnlock()

	return nodes, nil
}

// SearchByNodeClass finds all nodes of a specific class
func (ds *DiscoveryService) SearchByNodeClass(nodeClass string) ([]*NodeInfo, error) {
	return ds.SearchNodes("", nodeClass, 0)
}

// SearchByParent finds all child nodes of a specific parent
func (ds *DiscoveryService) SearchByParent(parentNodeID string) ([]*NodeInfo, error) {
	if !ds.config.EnableSearch || ds.index == nil {
		// Fallback to cache search
		return ds.searchByParentFromCache(parentNodeID)
	}

	// Search for nodes with specific parent
	query := bleve.NewTermQuery(parentNodeID)
	query.SetField("parent_node")

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = ds.config.SearchMaxResults

	searchResult, err := ds.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert search results to node info
	var nodes []*NodeInfo
	ds.cacheMutex.RLock()
	for _, hit := range searchResult.Hits {
		if nodeInfo, exists := ds.nodeCache[hit.ID]; exists {
			nodes = append(nodes, nodeInfo)
		}
	}
	ds.cacheMutex.RUnlock()

	return nodes, nil
}

// SearchByDepth finds all nodes at a specific depth
func (ds *DiscoveryService) SearchByDepth(depth int) ([]*NodeInfo, error) {
	if !ds.config.EnableSearch || ds.index == nil {
		// Fallback to cache search
		return ds.searchByDepthFromCache(depth)
	}

	// Search for nodes at specific depth
	depthFloat := float64(depth)
	query := bleve.NewNumericRangeQuery(&depthFloat, &depthFloat)
	query.SetField("depth")

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = ds.config.SearchMaxResults

	searchResult, err := ds.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert search results to node info
	var nodes []*NodeInfo
	ds.cacheMutex.RLock()
	for _, hit := range searchResult.Hits {
		if nodeInfo, exists := ds.nodeCache[hit.ID]; exists {
			nodes = append(nodes, nodeInfo)
		}
	}
	ds.cacheMutex.RUnlock()

	return nodes, nil
}

// GetNodeHierarchy returns the complete hierarchy path for a node
func (ds *DiscoveryService) GetNodeHierarchy(nodeID string) ([]*NodeInfo, error) {
	var hierarchy []*NodeInfo

	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	currentNodeID := nodeID
	for {
		nodeInfo, exists := ds.nodeCache[currentNodeID]
		if !exists {
			break
		}

		hierarchy = append([]*NodeInfo{nodeInfo}, hierarchy...)

		if nodeInfo.ParentNode == "" || nodeInfo.ParentNode == currentNodeID {
			break
		}
		currentNodeID = nodeInfo.ParentNode
	}

	return hierarchy, nil
}

// GetSearchStats returns search index statistics
func (ds *DiscoveryService) GetSearchStats() (map[string]interface{}, error) {
	if ds.index == nil {
		return map[string]interface{}{
			"index_enabled": false,
		}, nil
	}

	stats := map[string]interface{}{
		"index_enabled": true,
	}

	// Get index document count
	docCount, err := ds.index.DocCount()
	if err != nil {
		return nil, fmt.Errorf("failed to get document count: %w", err)
	}
	stats["indexed_documents"] = docCount

	// Get index size
	indexStats := ds.index.Stats()
	stats["index_stats"] = indexStats

	return stats, nil
}

// searchNodesFromCache performs multi-field search in cache
func (ds *DiscoveryService) searchNodesFromCache(query, nodeClass string, maxResults int) ([]*NodeInfo, error) {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	var nodes []*NodeInfo
	count := 0

	for _, nodeInfo := range ds.nodeCache {
		if maxResults > 0 && count >= maxResults {
			break
		}

		// Check node class filter
		if nodeClass != "" && nodeInfo.NodeClass != nodeClass {
			continue
		}

		// Check text query
		if query != "" {
			queryLower := strings.ToLower(query)
			browseNameLower := strings.ToLower(nodeInfo.BrowseName)
			displayNameLower := strings.ToLower(nodeInfo.DisplayName)

			if !strings.Contains(browseNameLower, queryLower) &&
				!strings.Contains(displayNameLower, queryLower) {
				continue
			}
		}

		nodes = append(nodes, nodeInfo)
		count++
	}

	return nodes, nil
}

// searchByParentFromCache searches for child nodes in cache
func (ds *DiscoveryService) searchByParentFromCache(parentNodeID string) ([]*NodeInfo, error) {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	var nodes []*NodeInfo
	for _, nodeInfo := range ds.nodeCache {
		if nodeInfo.ParentNode == parentNodeID {
			nodes = append(nodes, nodeInfo)
		}
	}

	return nodes, nil
}

// searchByDepthFromCache searches for nodes at specific depth in cache
func (ds *DiscoveryService) searchByDepthFromCache(depth int) ([]*NodeInfo, error) {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	var nodes []*NodeInfo
	for _, nodeInfo := range ds.nodeCache {
		if nodeInfo.Depth == depth {
			nodes = append(nodes, nodeInfo)
		}
	}

	return nodes, nil
}

// GetCacheStats returns cache statistics
func (ds *DiscoveryService) GetCacheStats() map[string]interface{} {
	ds.cacheMutex.RLock()
	defer ds.cacheMutex.RUnlock()

	// Count nodes by depth
	depthCounts := make(map[int]int)
	for _, nodeInfo := range ds.nodeCache {
		depthCounts[nodeInfo.Depth]++
	}

	return map[string]interface{}{
		"total_nodes":          len(ds.nodeCache),
		"cache_enabled":        ds.config.EnableCache,
		"search_enabled":       ds.config.EnableSearch,
		"discovery_enabled":    ds.config.EnableDiscovery,
		"max_discovery_depth":  ds.config.MaxDiscoveryDepth,
		"max_nodes_per_browse": ds.config.MaxNodesPerBrowse,
		"depth_distribution":   depthCounts,
	}
}

// ForceDiscoveryRefresh forces an immediate discovery refresh
func (ds *DiscoveryService) ForceDiscoveryRefresh(ctx context.Context) error {
	if !ds.config.EnableDiscovery {
		return fmt.Errorf("discovery is disabled")
	}

	logger.Info("Forcing discovery refresh")
	return ds.discoverNodes(ctx)
}

// EnsureServerNodesIndexed ensures that critical Server nodes are properly indexed
func (ds *DiscoveryService) EnsureServerNodesIndexed(ctx context.Context) error {
	// Check if Server node is already indexed
	ds.cacheMutex.RLock()
	_, serverExists := ds.nodeCache["i=2253"]
	ds.cacheMutex.RUnlock()

	if !serverExists {
		logger.Warn("Server node not found in cache, forcing discovery refresh")
		return ds.ForceDiscoveryRefresh(ctx)
	}

	// Check if ServerStatus node is indexed
	ds.cacheMutex.RLock()
	serverStatusExists := false
	for _, nodeInfo := range ds.nodeCache {
		if nodeInfo.NodeID == "i=2256" {
			serverStatusExists = true
			break
		}
	}
	ds.cacheMutex.RUnlock()

	if !serverStatusExists {
		logger.Warn("ServerStatus node not found in cache, forcing discovery refresh")
		return ds.ForceDiscoveryRefresh(ctx)
	}

	logger.Info("Server nodes are properly indexed")
	return nil
}

// DebugSearchNodes provides debugging information for search functionality
func (ds *DiscoveryService) DebugSearchNodes(browseName string) (map[string]interface{}, error) {
	debug := make(map[string]interface{})

	// Get cache stats
	debug["cache_stats"] = ds.GetCacheStats()

	// Get search stats
	if ds.index != nil {
		searchStats, err := ds.GetSearchStats()
		if err != nil {
			debug["search_stats_error"] = err.Error()
		} else {
			debug["search_stats"] = searchStats
		}
	} else {
		debug["search_index"] = "disabled"
	}

	// Search for exact matches in cache
	ds.cacheMutex.RLock()
	var exactMatches []*NodeInfo
	var partialMatches []*NodeInfo
	var timestampMatches []*NodeInfo

	for _, nodeInfo := range ds.nodeCache {
		// Exact match
		if nodeInfo.BrowseName == browseName {
			exactMatches = append(exactMatches, nodeInfo)
		}
		// Partial match (contains)
		if strings.Contains(strings.ToLower(nodeInfo.BrowseName), strings.ToLower(browseName)) {
			partialMatches = append(partialMatches, nodeInfo)
		}
		// Look for timestamp-related nodes
		if strings.Contains(strings.ToLower(nodeInfo.BrowseName), "timestamp") {
			timestampMatches = append(timestampMatches, nodeInfo)
		}
	}
	ds.cacheMutex.RUnlock()

	debug["exact_matches"] = len(exactMatches)
	debug["partial_matches"] = len(partialMatches)
	debug["timestamp_nodes"] = len(timestampMatches)

	// Show some timestamp nodes for reference
	if len(timestampMatches) > 0 {
		var timestampInfo []map[string]interface{}
		for i, node := range timestampMatches {
			if i >= 5 { // Limit to first 5
				break
			}
			timestampInfo = append(timestampInfo, map[string]interface{}{
				"node_id":      node.NodeID,
				"browse_name":  node.BrowseName,
				"display_name": node.DisplayName,
			})
		}
		debug["sample_timestamp_nodes"] = timestampInfo
	}

	// Test different search approaches
	debug["search_tests"] = make(map[string]interface{})

	// Test fuzzy search
	if ds.index != nil {
		fuzzyQuery := bleve.NewFuzzyQuery(browseName)
		fuzzyQuery.SetField("browse_name")
		fuzzyQuery.SetFuzziness(1)

		searchRequest := bleve.NewSearchRequest(fuzzyQuery)
		searchRequest.Size = 10

		searchResult, err := ds.index.Search(searchRequest)
		if err != nil {
			debug["search_tests"].(map[string]interface{})["fuzzy_search_error"] = err.Error()
		} else {
			debug["search_tests"].(map[string]interface{})["fuzzy_search_hits"] = len(searchResult.Hits)
			debug["search_tests"].(map[string]interface{})["fuzzy_search_total"] = searchResult.Total
		}

		// Test match query
		matchQuery := bleve.NewMatchQuery(browseName)
		matchQuery.SetField("browse_name")

		searchRequest2 := bleve.NewSearchRequest(matchQuery)
		searchRequest2.Size = 10

		searchResult2, err := ds.index.Search(searchRequest2)
		if err != nil {
			debug["search_tests"].(map[string]interface{})["match_search_error"] = err.Error()
		} else {
			debug["search_tests"].(map[string]interface{})["match_search_hits"] = len(searchResult2.Hits)
			debug["search_tests"].(map[string]interface{})["match_search_total"] = searchResult2.Total
		}

		// Test wildcard query
		wildcardQuery := bleve.NewWildcardQuery("*" + browseName + "*")
		wildcardQuery.SetField("browse_name")

		searchRequest3 := bleve.NewSearchRequest(wildcardQuery)
		searchRequest3.Size = 10

		searchResult3, err := ds.index.Search(searchRequest3)
		if err != nil {
			debug["search_tests"].(map[string]interface{})["wildcard_search_error"] = err.Error()
		} else {
			debug["search_tests"].(map[string]interface{})["wildcard_search_hits"] = len(searchResult3.Hits)
			debug["search_tests"].(map[string]interface{})["wildcard_search_total"] = searchResult3.Total
		}
	}

	return debug, nil
}
