package opcua

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

func newTestCacheStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "caching-test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("store.Open() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newEnabledSearchConfig() *config.SearchConfig {
	return &config.SearchConfig{EnableCache: true}
}

// flakyCacheStore wraps a real *store.Store, letting individual tests force
// exactly one method call to fail - for asserting CachingClient's
// error-falls-through-to-live behavior (NFR Design's error-handling pattern)
// without a hand-written store mock.
type flakyCacheStore struct {
	*store.Store
	failGetValue  bool
	failPutValue  bool
	failGetBrowse bool
	failGetType   bool
}

func (f *flakyCacheStore) GetValue(ctx context.Context, nodeID string) (store.ValueEntry, bool, error) {
	if f.failGetValue {
		f.failGetValue = false
		return store.ValueEntry{}, false, errors.New("injected GetValue failure")
	}
	return f.Store.GetValue(ctx, nodeID)
}

func (f *flakyCacheStore) PutValue(ctx context.Context, nodeID string, entry store.ValueEntry) error {
	if f.failPutValue {
		f.failPutValue = false
		return errors.New("injected PutValue failure")
	}
	return f.Store.PutValue(ctx, nodeID, entry)
}

func (f *flakyCacheStore) GetBrowse(ctx context.Context, parentNodeID string) (store.BrowseEntry, bool, error) {
	if f.failGetBrowse {
		f.failGetBrowse = false
		return store.BrowseEntry{}, false, errors.New("injected GetBrowse failure")
	}
	return f.Store.GetBrowse(ctx, parentNodeID)
}

func (f *flakyCacheStore) GetTypeInfo(ctx context.Context, nodeID string) (store.TypeInfoEntry, bool, error) {
	if f.failGetType {
		f.failGetType = false
		return store.TypeInfoEntry{}, false, errors.New("injected GetTypeInfo failure")
	}
	return f.Store.GetTypeInfo(ctx, nodeID)
}

func TestCachingClientReadDisabledBypassesCache(t *testing.T) {
	client := newTestClient(t)
	mock := &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusOK, Value: ua.MustVariant(int32(42))}}}, nil
	}}
	client.client = mock
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	cfg := &config.SearchConfig{EnableCache: false}
	cc := NewCachingClient(client, cache, cfg, time.Hour, time.Hour)

	results, err := cc.Read(context.Background(), []string{"i=1"}, 100000)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "live" {
		t.Fatalf("Read() = %+v, want single live result", results)
	}
	if got, _, _ := cache.GetValue(context.Background(), "i=1"); got.Value != nil {
		t.Errorf("EnableCache=false must not populate the cache, got %+v", got)
	}
}

func TestCachingClientReadSubscribedServedUnconditionally(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(7), Source: "subscription", ReceivedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	results, err := cc.Read(ctx, []string{"i=1"}, 0) // max_age_ms=0: would reject a "live"-sourced entry, but not a subscribed one
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "subscription" || results[0].Value != int32(7) {
		t.Fatalf("Read() = %+v, want subscription-sourced result served regardless of max_age_ms", results)
	}
}

func TestCachingClientReadCacheHitWithinTTL(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(9), Source: "live", ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	results, err := cc.Read(ctx, []string{"i=1"}, 60000)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "cache" {
		t.Fatalf("Read() = %+v, want cache-sourced result", results)
	}
}

func TestCachingClientReadCacheExpiredFallsThroughToLive(t *testing.T) {
	client := newTestClient(t)
	readCalls := 0
	client.client = &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		readCalls++
		return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusOK, Value: ua.MustVariant(int32(99))}}}, nil
	}}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "live", ReceivedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	results, err := cc.Read(ctx, []string{"i=1"}, 1000) // 1s tolerance, entry is 1h stale
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "live" || results[0].Value != int32(99) {
		t.Fatalf("Read() = %+v, want live result for expired cache entry", results)
	}
	if readCalls != 1 {
		t.Errorf("Client.Read called %d times, want 1", readCalls)
	}

	entry, ok, err := cache.GetValue(ctx, "i=1")
	if err != nil || !ok {
		t.Fatalf("expected opportunistic re-cache, got ok=%v err=%v", ok, err)
	}
	if entry.Source != "live" || entry.Value != int32(99) {
		t.Errorf("re-cached entry = %+v, want fresh live value", entry)
	}
}

func TestCachingClientReadStoreErrorFallsThroughToLive(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusOK, Value: ua.MustVariant(int32(5))}}}, nil
	}}
	client.SetConnectedForTesting(true)

	cache := &flakyCacheStore{Store: newTestCacheStore(t), failGetValue: true}
	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)

	results, err := cc.Read(context.Background(), []string{"i=1"}, 60000)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "live" {
		t.Fatalf("Read() = %+v, want live fallback on a store read error", results)
	}
}

func TestCachingClientBrowseCacheHitWithinTTL(t *testing.T) {
	client := newTestClient(t)
	browseCalls := 0
	client.client = &mockOpcuaClient{browseFunc: func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
		browseCalls++
		return &ua.BrowseResponse{}, nil
	}}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	want := []store.BrowseReference{{NodeID: "i=2", BrowseName: "Foo"}}
	if err := cache.PutBrowse(ctx, "i=85", store.BrowseEntry{References: want, CachedAt: time.Now()}); err != nil {
		t.Fatalf("PutBrowse() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	refs, err := cc.Browse(ctx, "i=85")
	if err != nil {
		t.Fatalf("Browse() error: %v", err)
	}
	if len(refs) != 1 || refs[0].NodeID != "i=2" {
		t.Fatalf("Browse() = %+v, want cached references", refs)
	}
	if browseCalls != 0 {
		t.Errorf("Client.Browse called %d times, want 0 (cache hit)", browseCalls)
	}
}

func TestCachingClientBrowseCacheMissGoesLiveAndCaches(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{browseFunc: func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
		return &ua.BrowseResponse{
			Results: []*ua.BrowseResult{{
				StatusCode: ua.StatusOK,
				References: []*ua.ReferenceDescription{{
					NodeID:         ua.NewTwoByteExpandedNodeID(2),
					BrowseName:     &ua.QualifiedName{Name: "Foo"},
					DisplayName:    &ua.LocalizedText{Text: "Foo"},
					NodeClass:      ua.NodeClassVariable,
					TypeDefinition: ua.NewTwoByteExpandedNodeID(63),
				}},
			}},
		}, nil
	}}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)

	refs, err := cc.Browse(ctx, "i=85")
	if err != nil {
		t.Fatalf("Browse() error: %v", err)
	}
	if len(refs) != 1 || refs[0].BrowseName != "Foo" {
		t.Fatalf("Browse() = %+v, want live-converted references", refs)
	}

	entry, ok, err := cache.GetBrowse(ctx, "i=85")
	if err != nil || !ok || len(entry.References) != 1 {
		t.Fatalf("expected browse result to be cached, got ok=%v err=%v entry=%+v", ok, err, entry)
	}
}

func TestCachingClientGetNodeTypeInfoCacheHitWithinTTL(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{} // must not be called on a cache hit
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutTypeInfo(ctx, "i=1", store.TypeInfoEntry{
		DataTypeID: uint32(ua.TypeIDInt32), ValueRank: -1, AccessLevel: 3, UserAccessLevel: 3, CachedAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutTypeInfo() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	info, err := cc.GetNodeTypeInfo(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetNodeTypeInfo() error: %v", err)
	}
	if info.DataType.IntID() != uint32(ua.TypeIDInt32) || !info.IsScalar {
		t.Errorf("GetNodeTypeInfo() = %+v, want reconstructed cached entry", info)
	}
}

func TestCachingClientGetNodeTypeInfoCacheMissGoesLiveAndCaches(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDDouble)), 3)}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)

	info, err := cc.GetNodeTypeInfo(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetNodeTypeInfo() error: %v", err)
	}
	if info.DataType.IntID() != uint32(ua.TypeIDDouble) {
		t.Fatalf("GetNodeTypeInfo() = %+v, want live Double type", info)
	}

	entry, ok, err := cache.GetTypeInfo(ctx, "i=1")
	if err != nil || !ok || entry.DataTypeID != uint32(ua.TypeIDDouble) {
		t.Fatalf("expected type info to be cached, got ok=%v err=%v entry=%+v", ok, err, entry)
	}
}

func TestCachingClientWriteInvalidatesNonSubscribedEntry(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), 3)}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "live", ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	if err := cc.Write(ctx, "i=1", 5); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if _, ok, err := cache.GetValue(ctx, "i=1"); err != nil || ok {
		t.Errorf("expected non-subscribed cache entry to be invalidated after write, ok=%v err=%v", ok, err)
	}
}

func TestCachingClientWriteLeavesSubscribedEntryAlone(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), 3)}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "subscription", ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	if err := cc.Write(ctx, "i=1", 5); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	entry, ok, err := cache.GetValue(ctx, "i=1")
	if err != nil || !ok || entry.Source != "subscription" {
		t.Errorf("expected subscribed cache entry to survive a write, ok=%v err=%v entry=%+v", ok, err, entry)
	}
}

func TestCachingClientWriteErrorSkipsInvalidation(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), 0)} // AccessLevel 0: not writable, Write() fails validation
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "live", ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	if err := cc.Write(ctx, "i=1", 5); err == nil {
		t.Fatal("Write() expected an error for a non-writable node")
	}

	if _, ok, err := cache.GetValue(ctx, "i=1"); err != nil || !ok {
		t.Errorf("a failed Write() must not invalidate the cache, ok=%v err=%v", ok, err)
	}
}
