package opcua

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

// treeBrowser simulates an OPC-UA address space for discovery tests: a
// mutable adjacency map from parent node ID to its children, browsed through
// the same opcuaClient mock seam client_test.go uses (mockOpcuaClient's
// browseFunc), so DiscoveryService is exercised via the real Client.Browse
// (including its BrowseNext loop) rather than a discovery-specific seam.
type treeBrowser struct {
	mu          sync.Mutex
	children    map[string][]string
	browseCalls map[string]int
	browseNames map[string]string // nodeID -> BrowseName override; defaults to the node ID itself
}

func newTreeBrowser() *treeBrowser {
	return &treeBrowser{
		children:    make(map[string][]string),
		browseCalls: make(map[string]int),
		browseNames: make(map[string]string),
	}
}

func (tb *treeBrowser) setChildren(parent string, children ...string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.children[parent] = children
}

// setBrowseName overrides the BrowseName reported for nodeID (default: the
// node ID string itself). Useful for Bleve-backed tests, since its "en" text
// analyzer doesn't tokenize node-ID-shaped strings like "i=2" predictably.
func (tb *treeBrowser) setBrowseName(nodeID, browseName string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.browseNames[nodeID] = browseName
}

func (tb *treeBrowser) callCount(nodeID string) int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.browseCalls[nodeID]
}

func (tb *treeBrowser) browseFunc(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
	nodeID := req.NodesToBrowse[0].NodeID.String()

	tb.mu.Lock()
	tb.browseCalls[nodeID]++
	children := append([]string(nil), tb.children[nodeID]...)
	names := tb.browseNames
	tb.mu.Unlock()

	refs := make([]*ua.ReferenceDescription, len(children))
	for i, childID := range children {
		id, err := ua.ParseNodeID(childID)
		if err != nil {
			return nil, err
		}
		browseName := childID
		if override, ok := names[childID]; ok {
			browseName = override
		}
		refs[i] = &ua.ReferenceDescription{
			NodeID:      ua.NewExpandedNodeID(id, "", 0),
			BrowseName:  &ua.QualifiedName{Name: browseName},
			DisplayName: &ua.LocalizedText{Text: browseName},
			NodeClass:   ua.NodeClassObject,
			// TypeDefinition.String() is a value-receiver method on
			// *ExpandedNodeID; leaving this nil panics (a real server
			// response always populates it when ResultMaskAll is
			// requested, as Browse does).
			TypeDefinition: ua.NewExpandedNodeID(ua.NewNumericNodeID(0, 58), "", 0), // BaseObjectType
		}
	}

	return &ua.BrowseResponse{
		Results: []*ua.BrowseResult{{StatusCode: ua.StatusOK, References: refs}},
	}, nil
}

// newTestDiscoveryService builds a DiscoveryService backed by a connected
// Client whose Browse calls are served by tb, with search/discovery
// intervals irrelevant since discoverNodes is driven directly in tests
// rather than through the ticker.
func newTestDiscoveryService(t *testing.T, tb *treeBrowser) *DiscoveryService {
	t.Helper()

	client := newTestClient(t)
	client.client = &mockOpcuaClient{browseFunc: tb.browseFunc}
	client.connected = true

	cfg := &config.SearchConfig{
		EnableDiscovery:   true,
		DiscoveryRootNode: "i=85",
		MaxDiscoveryDepth: 10,
		MaxNodesPerBrowse: 10000,
		EnableSearch:      false, // exercise the in-memory cache path; Bleve is covered separately
		SearchMaxResults:  100,
	}

	ds, err := NewDiscoveryService(client, cfg)
	if err != nil {
		t.Fatalf("NewDiscoveryService() error: %v", err)
	}
	return ds
}

// TestDiscoverNodesSweepsRemovedNodes covers P1-3 (findings.md H6/H7/H8): a
// node present in one discovery cycle and absent in the next must be removed
// from nodeCache after the second cycle completes.
func TestDiscoverNodesSweepsRemovedNodes(t *testing.T) {
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1", "i=2")
	tb.setChildren("i=1")
	tb.setChildren("i=2")

	ds := newTestDiscoveryService(t, tb)
	ctx := context.Background()

	if err := ds.discoverNodes(ctx); err != nil {
		t.Fatalf("first discoverNodes() error: %v", err)
	}
	if _, err := ds.GetNodeInfo("i=2"); err != nil {
		t.Fatalf("i=2 expected in cache after first walk: %v", err)
	}

	// i=2 no longer exists on the live server.
	tb.setChildren("i=85", "i=1")

	if err := ds.discoverNodes(ctx); err != nil {
		t.Fatalf("second discoverNodes() error: %v", err)
	}

	if _, err := ds.GetNodeInfo("i=2"); err == nil {
		t.Error("i=2 still in cache after being removed from the live tree")
	}
	if _, err := ds.GetNodeInfo("i=1"); err != nil {
		t.Errorf("i=1 should still be in cache: %v", err)
	}
}

// TestDiscoverNodesDoesNotRewalkSharedSubtree covers P1-3's H6 fix: a node
// reachable via two different parents (a graph, not a tree) must have its
// own children browsed only once per generation.
func TestDiscoverNodesDoesNotRewalkSharedSubtree(t *testing.T) {
	tb := newTreeBrowser()
	// i=1 and i=2 both reference the shared node i=99.
	tb.setChildren("i=85", "i=1", "i=2")
	tb.setChildren("i=1", "i=99")
	tb.setChildren("i=2", "i=99")
	tb.setChildren("i=99", "i=100")

	ds := newTestDiscoveryService(t, tb)

	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}

	if got := tb.callCount("i=99"); got != 1 {
		t.Errorf("Browse(i=99) called %d times, want exactly 1 (shared subtree re-walked)", got)
	}
	if _, err := ds.GetNodeInfo("i=100"); err != nil {
		t.Errorf("i=100 (child of the shared node) should be in cache: %v", err)
	}
}

// TestDiscoverNodesMaxNodesPerBrowsePerLevel covers P1-5 (findings.md H10e):
// the per-node sibling counter must only count direct children at the
// current level, not each sibling's entire recursive subtree size. A tree
// where the first of 5 siblings alone has far more descendants than
// MaxNodesPerBrowse must not starve the remaining siblings.
func TestDiscoverNodesMaxNodesPerBrowsePerLevel(t *testing.T) {
	tb := newTreeBrowser()

	siblings := []string{"i=1", "i=2", "i=3", "i=4", "i=5"}
	tb.setChildren("i=85", siblings...)

	// i=1 alone has 9999 descendants (a long chain), while i=2..i=5 have none.
	const bigSubtreeSize = 9999
	prev := "i=1"
	for i := 0; i < bigSubtreeSize; i++ {
		child := "s=chain" + strconv.Itoa(i)
		tb.setChildren(prev, child)
		prev = child
	}

	ds := newTestDiscoveryService(t, tb)
	ds.config.MaxNodesPerBrowse = 10000
	// MaxDiscoveryDepth must be deep enough that the chain under i=1 actually
	// produces ~9999 discovered descendants - otherwise depth cuts the walk
	// short long before MaxNodesPerBrowse could ever be crossed, and this
	// test would pass even against the old buggy accounting.
	ds.config.MaxDiscoveryDepth = bigSubtreeSize + 10

	if err := ds.discoverNodes(context.Background()); err != nil {
		t.Fatalf("discoverNodes() error: %v", err)
	}

	for _, sib := range siblings {
		if _, err := ds.GetNodeInfo(sib); err != nil {
			t.Errorf("sibling %s should have been discovered (MaxNodesPerBrowse must count direct children only): %v", sib, err)
		}
	}
}

// TestDiscoverNodesConcurrentReadDuringWalk covers P1-3's H7 acceptance
// criterion under -race: a concurrent GetNodeInfo call issued mid-walk must
// never see a "not found" result for a node that was valid before the walk
// started and is still present in the live tree, since discoverNodes no
// longer wipes nodeCache before rebuilding it.
func TestDiscoverNodesConcurrentReadDuringWalk(t *testing.T) {
	tb := newTreeBrowser()
	tb.setChildren("i=85", "i=1")
	tb.setChildren("i=1", "i=2")
	tb.setChildren("i=2", "i=3")

	ds := newTestDiscoveryService(t, tb)
	ctx := context.Background()

	if err := ds.discoverNodes(ctx); err != nil {
		t.Fatalf("initial discoverNodes() error: %v", err)
	}

	stop := make(chan struct{})
	var readErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := ds.GetNodeInfo("i=1"); err != nil {
					readErr = err
					return
				}
			}
		}
	}()

	for i := 0; i < 20; i++ {
		if err := ds.discoverNodes(ctx); err != nil {
			t.Fatalf("discoverNodes() error on iteration %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	if readErr != nil {
		t.Errorf("GetNodeInfo(i=1) failed mid-walk even though the node is always present: %v", readErr)
	}
}

// bleveDocIDs returns every document ID currently in the index, via a
// MatchAllQuery. Deliberately avoids GetNodeByBrowseName/MatchQuery: those go
// through a browse_name field that's mapped with both a text and a keyword
// analyzer on the same path (discovery.go's NewDiscoveryService), which
// leaves bleve.NewMatchQuery unable to resolve a single analyzer for that
// field and return hits - a real, pre-existing, separately-scoped bug (not a
// side effect of P1-3) noticed while writing this test but not fixed here.
func bleveDocIDs(t *testing.T, ds *DiscoveryService) map[string]bool {
	t.Helper()
	req := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	req.Size = 1000
	result, err := ds.index.Search(req)
	if err != nil {
		t.Fatalf("bleve MatchAllQuery error: %v", err)
	}
	ids := make(map[string]bool, len(result.Hits))
	for _, hit := range result.Hits {
		ids[hit.ID] = true
	}
	return ids
}

// TestDiscoverNodesSweepsBleveIndex covers P1-3's H8 acceptance criterion
// directly against Bleve (the other tests above exercise only the in-memory
// cache path): a node removed between two discovery cycles must no longer
// exist in the Bleve index after the second cycle completes.
func TestDiscoverNodesSweepsBleveIndex(t *testing.T) {
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
	defer ds.index.Close()

	ctx := context.Background()
	if err := ds.discoverNodes(ctx); err != nil {
		t.Fatalf("first discoverNodes() error: %v", err)
	}
	if ids := bleveDocIDs(t, ds); !ids["i=2"] {
		t.Fatalf("i=2 expected in Bleve index after first walk, got IDs: %v", ids)
	}

	// i=2 (BetaNode) no longer exists on the live server.
	tb.setChildren("i=85", "i=1")

	if err := ds.discoverNodes(ctx); err != nil {
		t.Fatalf("second discoverNodes() error: %v", err)
	}

	ids := bleveDocIDs(t, ds)
	if ids["i=2"] {
		t.Error("i=2 still in Bleve index after being removed from the live tree and swept")
	}
	if !ids["i=1"] {
		t.Errorf("i=1 should still be in Bleve index, got IDs: %v", ids)
	}
}
