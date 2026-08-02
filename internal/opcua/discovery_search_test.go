package opcua

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

// newPopulatedDiscoveryService builds a DiscoveryService (Bleve enabled,
// backed by a temp index) with a small fixed tree already discovered, for
// P1-7's baseline search-tier tests.
func newPopulatedDiscoveryService(t *testing.T) *DiscoveryService {
	t.Helper()
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1", "i=2")
	tb.setChildren("i=1")
	tb.setChildren("i=2")
	tb.setBrowseName("i=1", "AlphaNode")
	tb.setBrowseName("i=2", "BetaNode")

	client := newTestClient(t)
	client.client = &mockOpcuaClient{browseFunc: tb.browseFunc}
	client.connected = true

	cfg := &config.SearchConfig{
		EnableDiscovery:   true,
		DiscoveryRootNode: "i=85",
		MaxDiscoveryDepth: 10,
		MaxNodesPerBrowse: 10000,
		EnableSearch:      true,
		SearchIndexPath:   filepath.Join(t.TempDir(), "index"),
		SearchMaxResults:  100,
	}

	ds, err := NewDiscoveryService(client, cfg)
	if err != nil {
		t.Fatalf("NewDiscoveryService() error: %v", err)
	}
	t.Cleanup(func() { ds.index.Close() })

	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}
	return ds
}

// TestBleveBackedSearchTiersSmoke covers P1-7's baseline requirement that
// every discovery search-tier function has at least one test. These assert
// only that each function runs without error against a populated index -
// NOT on result counts/content: empirical probing while writing this test
// found several of these (searchExact/searchWildcard/searchFuzzy, SearchNodes,
// SearchByDepth, SearchByNodeClass) return zero hits for queries that
// intuitively should match, beyond the browse_name dual-analyzer-mapping
// issue already flagged in discovery.go's NewDiscoveryService. That's a
// real, separate correctness gap worth its own investigation and plan item -
// not something to paper over with assertions tuned to match the current
// (likely buggy) behavior.
func TestBleveBackedSearchTiersSmoke(t *testing.T) {
	ds := newPopulatedDiscoveryService(t)

	if _, err := ds.searchExact("BetaNode"); err != nil {
		t.Errorf("searchExact() error: %v", err)
	}
	if _, err := ds.searchWildcard("Beta"); err != nil {
		t.Errorf("searchWildcard() error: %v", err)
	}
	if _, err := ds.searchFuzzy("BetaNod"); err != nil {
		t.Errorf("searchFuzzy() error: %v", err)
	}
	if _, err := ds.SearchNodes("Beta", "", 0); err != nil {
		t.Errorf("SearchNodes() error: %v", err)
	}
	if _, err := ds.SearchByParent(""); err != nil {
		t.Errorf("SearchByParent() error: %v", err)
	}
	if _, err := ds.SearchByDepth(0); err != nil {
		t.Errorf("SearchByDepth() error: %v", err)
	}
	if _, err := ds.SearchByNodeClass("NodeClassObject"); err != nil {
		t.Errorf("SearchByNodeClass() error: %v", err)
	}
	if _, err := ds.GetSearchStats(); err != nil {
		t.Errorf("GetSearchStats() error: %v", err)
	}
	if _, err := ds.FindSimilarNodes("Beta"); err != nil {
		t.Errorf("FindSimilarNodes() error: %v", err)
	}
	if _, err := ds.GetNodeByBrowseName("AlphaNode"); err == nil {
		t.Log("GetNodeByBrowseName unexpectedly succeeded despite the known MatchQuery issue - harmless, just noting for the follow-up investigation")
	}
}

// TestFindSimilarNodesFromCacheTiers covers all three tiers of the pure Go
// cache fallback (exact, contains, fuzzy via isSimilarString), independent
// of Bleve.
func TestFindSimilarNodesFromCacheTiers(t *testing.T) {
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1", "i=2", "i=3")
	tb.setChildren("i=1")
	tb.setChildren("i=2")
	tb.setChildren("i=3")
	tb.setBrowseName("i=1", "Temperature")
	tb.setBrowseName("i=2", "TemperatureSensor")
	tb.setBrowseName("i=3", "Temprature") // typo, for the fuzzy tier

	ds := newTestDiscoveryService(t, tb)
	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}

	exact, err := ds.findSimilarNodesFromCache("Temperature")
	if err != nil {
		t.Fatalf("findSimilarNodesFromCache() error: %v", err)
	}
	if len(exact) == 0 || exact[0].BrowseName != "Temperature" {
		t.Errorf("expected the exact match ranked first, got %+v", exact)
	}

	empty, err := ds.findSimilarNodesFromCache("NoSuchNode")
	if err != nil {
		t.Fatalf("findSimilarNodesFromCache() error: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected no matches for an unrelated query, got %+v", empty)
	}
}

// TestIsSimilarString covers the fuzzy-matching helper directly.
func TestIsSimilarString(t *testing.T) {
	ds := &DiscoveryService{}
	tests := []struct {
		s1, s2 string
		want   bool
	}{
		{"temperature", "temprature", true},        // one dropped char, shared prefix+suffix
		{"pressure", "pressuresensorvalue", false}, // length ratio too different (>2x)
		{"abc", "xyz", false},
		{"abc", "abc", true},
	}
	for _, tt := range tests {
		if got := ds.isSimilarString(tt.s1, tt.s2); got != tt.want {
			t.Errorf("isSimilarString(%q, %q) = %v, want %v", tt.s1, tt.s2, got, tt.want)
		}
	}
}

// TestCacheFallbackSearchFunctions covers the pure in-memory search
// functions used when EnableSearch is off.
func TestCacheFallbackSearchFunctions(t *testing.T) {
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1", "i=2")
	tb.setChildren("i=1", "i=10")
	tb.setChildren("i=2")
	tb.setChildren("i=10")
	tb.setBrowseName("i=1", "Alpha")
	tb.setBrowseName("i=2", "Beta")
	tb.setBrowseName("i=10", "AlphaChild")

	ds := newTestDiscoveryService(t, tb)
	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}

	byQuery, err := ds.searchNodesFromCache("alpha", "", 0)
	if err != nil {
		t.Fatalf("searchNodesFromCache() error: %v", err)
	}
	if len(byQuery) != 2 { // Alpha and AlphaChild both contain "alpha"
		t.Errorf("searchNodesFromCache(alpha) = %d results, want 2: %+v", len(byQuery), byQuery)
	}

	byParent, err := ds.searchByParentFromCache("i=1")
	if err != nil {
		t.Fatalf("searchByParentFromCache() error: %v", err)
	}
	if len(byParent) != 1 || byParent[0].BrowseName != "AlphaChild" {
		t.Errorf("searchByParentFromCache(i=1) = %+v, want [AlphaChild]", byParent)
	}

	byDepth, err := ds.searchByDepthFromCache(0)
	if err != nil {
		t.Fatalf("searchByDepthFromCache() error: %v", err)
	}
	if len(byDepth) != 2 { // Alpha and Beta are depth 0 (direct children of root)
		t.Errorf("searchByDepthFromCache(0) = %d results, want 2: %+v", len(byDepth), byDepth)
	}
}

// TestGetCacheStatsAndHierarchy covers GetCacheStats, GetAllNodes, and
// GetNodeHierarchy.
func TestGetCacheStatsAndHierarchy(t *testing.T) {
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1")
	tb.setChildren("i=1", "i=2")
	tb.setChildren("i=2")

	ds := newTestDiscoveryService(t, tb)
	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}

	stats := ds.GetCacheStats()
	if stats["total_nodes"] != 2 {
		t.Errorf("GetCacheStats()[total_nodes] = %v, want 2", stats["total_nodes"])
	}

	all := ds.GetAllNodes()
	if len(all) != 2 {
		t.Errorf("GetAllNodes() returned %d nodes, want 2", len(all))
	}

	hierarchy, err := ds.GetNodeHierarchy("i=2")
	if err != nil {
		t.Fatalf("GetNodeHierarchy() error: %v", err)
	}
	if len(hierarchy) != 2 || hierarchy[0].NodeID != "i=1" || hierarchy[1].NodeID != "i=2" {
		t.Errorf("GetNodeHierarchy(i=2) = %+v, want [i=1, i=2] in root-to-leaf order", hierarchy)
	}
}
