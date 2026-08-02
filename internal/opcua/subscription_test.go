package opcua

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

func newTestStoreConfig(t *testing.T) *config.StoreConfig {
	t.Helper()
	return &config.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "test.db"),
		OpenTimeout:      5 * time.Second,
		TypeInfoTTL:      24 * time.Hour,
		BrowseTTL:        5 * time.Minute,
		BatchWindow:      10 * time.Millisecond,
		BatchMaxItems:    250,
		NotifyChanBuffer: 1024,
	}
}

func newTestStoreForSubscription(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "sub-test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("store.Open() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSubscribeSuccess(t *testing.T) {
	client := &mockSubscribingClient{}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	id, rejected, err := mgr.Subscribe(ctx, []string{"i=1", "i=2"}, 1000)
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	if len(rejected) != 0 {
		t.Errorf("Subscribe() rejected = %+v, want none", rejected)
	}
	if id == "" {
		t.Error("Subscribe() returned empty id")
	}

	list := mgr.ListSubscriptions()
	if len(list) != 1 || list[0].ID != id || len(list[0].NodeIDs) != 2 {
		t.Errorf("ListSubscriptions() = %+v, want one entry with 2 nodes", list)
	}

	intents, err := cache.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("cache.ListSubscriptions() error: %v", err)
	}
	if len(intents) != 1 || intents[0].ID != id {
		t.Errorf("persisted intents = %+v, want one matching %s", intents, id)
	}
}

func TestSubscribePartialFailure(t *testing.T) {
	client := &mockSubscribingClient{
		subscribeFunc: func(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error) {
			return &mockSubscriptionHandle{
				monitorFunc: func(ctx context.Context, ts ua.TimestampsToReturn, items ...*ua.MonitoredItemCreateRequest) (*ua.CreateMonitoredItemsResponse, error) {
					results := make([]*ua.MonitoredItemCreateResult, len(items))
					for i, item := range items {
						if item.ItemToMonitor.NodeID.String() == "i=2" {
							results[i] = &ua.MonitoredItemCreateResult{StatusCode: ua.StatusBadNodeIDUnknown}
							continue
						}
						results[i] = &ua.MonitoredItemCreateResult{StatusCode: ua.StatusOK, MonitoredItemID: uint32(i + 1)} //nolint:gosec
					}
					return &ua.CreateMonitoredItemsResponse{Results: results}, nil
				},
			}, nil
		},
	}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	id, rejected, err := mgr.Subscribe(ctx, []string{"i=1", "i=2", "i=3"}, 1000)
	if err != nil {
		t.Fatalf("Subscribe() with a partial failure returned an error: %v", err)
	}
	if len(rejected) != 1 || rejected[0].NodeID != "i=2" {
		t.Errorf("Subscribe() rejected = %+v, want exactly [i=2]", rejected)
	}

	list := mgr.ListSubscriptions()
	if len(list) != 1 || len(list[0].NodeIDs) != 2 {
		t.Fatalf("ListSubscriptions() = %+v, want one entry with 2 accepted nodes", list)
	}
	for _, nodeID := range list[0].NodeIDs {
		if nodeID == "i=2" {
			t.Errorf("rejected node i=2 was persisted as accepted: %+v", list[0])
		}
	}
	_ = id
}

func TestSubscribeAllRejected(t *testing.T) {
	client := &mockSubscribingClient{
		subscribeFunc: func(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error) {
			return &mockSubscriptionHandle{
				monitorFunc: func(ctx context.Context, ts ua.TimestampsToReturn, items ...*ua.MonitoredItemCreateRequest) (*ua.CreateMonitoredItemsResponse, error) {
					results := make([]*ua.MonitoredItemCreateResult, len(items))
					for i := range items {
						results[i] = &ua.MonitoredItemCreateResult{StatusCode: ua.StatusBadNodeIDUnknown}
					}
					return &ua.CreateMonitoredItemsResponse{Results: results}, nil
				},
			}, nil
		},
	}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	_, rejected, err := mgr.Subscribe(ctx, []string{"i=1"}, 1000)
	if err == nil {
		t.Fatal("Subscribe() with every node rejected expected an error, got nil")
	}
	if len(rejected) != 1 {
		t.Errorf("Subscribe() rejected = %+v, want 1 entry", rejected)
	}
	if len(mgr.ListSubscriptions()) != 0 {
		t.Errorf("ListSubscriptions() = %+v, want none after an all-rejected Subscribe", mgr.ListSubscriptions())
	}
}

func TestSubscribeSharesIntervalGroup(t *testing.T) {
	client := &mockSubscribingClient{}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	if _, _, err := mgr.Subscribe(ctx, []string{"i=1"}, 1000); err != nil {
		t.Fatalf("first Subscribe() error: %v", err)
	}
	if _, _, err := mgr.Subscribe(ctx, []string{"i=2"}, 1000); err != nil {
		t.Fatalf("second Subscribe() error: %v", err)
	}
	if _, _, err := mgr.Subscribe(ctx, []string{"i=3"}, 500); err != nil {
		t.Fatalf("third Subscribe() (different interval) error: %v", err)
	}

	if client.subscribeCalls != 2 {
		t.Errorf("underlying client.Subscribe called %d times, want exactly 2 (one per distinct interval: 1000ms shared, 500ms separate)", client.subscribeCalls)
	}
}

func TestUnsubscribeDecrementsRefCountAndCancelsAtZero(t *testing.T) {
	var cancelCalls int
	handle := &mockSubscriptionHandle{
		cancelFunc: func(ctx context.Context) error {
			cancelCalls++
			return nil
		},
	}
	client := &mockSubscribingClient{
		subscribeFunc: func(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error) {
			return handle, nil
		},
	}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	id1, _, err := mgr.Subscribe(ctx, []string{"i=1"}, 1000)
	if err != nil {
		t.Fatalf("first Subscribe() error: %v", err)
	}
	id2, _, err := mgr.Subscribe(ctx, []string{"i=2"}, 1000)
	if err != nil {
		t.Fatalf("second Subscribe() error: %v", err)
	}

	if err := mgr.Unsubscribe(ctx, id1); err != nil {
		t.Fatalf("Unsubscribe(id1) error: %v", err)
	}
	if cancelCalls != 0 {
		t.Errorf("Cancel() called after removing only one of two subscriptions sharing the interval, want 0 calls")
	}
	if len(mgr.ListSubscriptions()) != 1 {
		t.Errorf("ListSubscriptions() = %+v, want 1 remaining", mgr.ListSubscriptions())
	}

	if err := mgr.Unsubscribe(ctx, id2); err != nil {
		t.Fatalf("Unsubscribe(id2) error: %v", err)
	}
	if cancelCalls != 1 {
		t.Errorf("Cancel() called %d times after removing the last subscription for this interval, want 1", cancelCalls)
	}
	if len(mgr.ListSubscriptions()) != 0 {
		t.Errorf("ListSubscriptions() = %+v, want none", mgr.ListSubscriptions())
	}

	intents, err := cache.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("cache.ListSubscriptions() error: %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("persisted intents = %+v, want none after both unsubscribed", intents)
	}
}

func TestUnsubscribeNotFound(t *testing.T) {
	client := &mockSubscribingClient{}
	cache := newTestStoreForSubscription(t)
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	if err := mgr.Unsubscribe(ctx, "does-not-exist"); err == nil {
		t.Error("Unsubscribe() for an unknown ID expected an error, got nil")
	}
}

func TestWarmStartRestoresPersistedIntentBeforeStartReturns(t *testing.T) {
	cache := newTestStoreForSubscription(t)
	ctx := context.Background()

	// Pre-populate the store with a subscription intent, as if from a prior run.
	if err := cache.PutSubscription(ctx, "sub-1", store.SubscriptionIntent{
		ID: "sub-1", NodeIDs: []string{"i=1", "i=2"}, IntervalMs: 1000, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutSubscription() error: %v", err)
	}

	client := &mockSubscribingClient{}
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	// By the time Start() returns, the warm-start must already be reflected
	// in-memory (BR-7) - no polling/waiting needed here.
	list := mgr.ListSubscriptions()
	if len(list) != 1 || list[0].ID != "sub-1" || len(list[0].NodeIDs) != 2 {
		t.Errorf("ListSubscriptions() immediately after Start() = %+v, want the restored sub-1", list)
	}
	if client.subscribeCalls != 1 {
		t.Errorf("client.Subscribe called %d times during warm-start, want 1", client.subscribeCalls)
	}
}

func TestPumpBatchesNotificationsIntoStore(t *testing.T) {
	client := &mockSubscribingClient{}
	cache := newTestStoreForSubscription(t)
	cfg := newTestStoreConfig(t)
	cfg.BatchWindow = 10 * time.Millisecond
	mgr := NewSubscriptionManager(client, cache, cfg)

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	if _, _, err := mgr.Subscribe(ctx, []string{"i=1"}, 1000); err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}

	mgr.notifyCh <- &opcua.PublishNotificationData{
		Value: &ua.DataChangeNotification{
			MonitoredItems: []*ua.MonitoredItemNotification{
				{ClientHandle: 1, Value: &ua.DataValue{Status: ua.StatusOK, Value: ua.MustVariant(int32(42))}},
			},
		},
	}

	waitForCondition(t, func() bool {
		entry, ok, err := cache.GetValue(ctx, "i=1")
		return err == nil && ok && entry.Value == int32(42) && entry.Source == "subscription"
	})
}

// flakyStore wraps a real *store.Store, failing PutValue exactly once (on
// its first call) - used to verify BR-6 (the pump logs and continues on a
// store write failure rather than crashing) against a realistic failure
// mode, since a real OPC-UA notification can never carry a value type
// store.encode() itself would reject (both sets mirror the same OPC-UA
// scalar types).
type flakyStore struct {
	*store.Store
	failedOnce bool
}

func (f *flakyStore) PutValue(ctx context.Context, nodeID string, entry store.ValueEntry) error {
	if !f.failedOnce {
		f.failedOnce = true
		return errors.New("simulated store failure")
	}
	return f.Store.PutValue(ctx, nodeID, entry)
}

func TestPumpStoreWriteFailureDoesNotCrashPump(t *testing.T) {
	client := &mockSubscribingClient{}
	cache := &flakyStore{Store: newTestStoreForSubscription(t)}
	mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer mgr.Stop()

	if _, _, err := mgr.Subscribe(ctx, []string{"i=1"}, 1000); err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}

	// First notification's PutValue fails (flakyStore.failedOnce) - the pump
	// must log and continue (BR-6), not crash. Wait past BatchWindow so it
	// flushes (and fails) in its own batch, separate from the second
	// notification below - otherwise both would coalesce into a single
	// batched write for the same node ID.
	mgr.notifyCh <- &opcua.PublishNotificationData{
		Value: &ua.DataChangeNotification{
			MonitoredItems: []*ua.MonitoredItemNotification{
				{ClientHandle: 1, Value: &ua.DataValue{Status: ua.StatusOK, Value: ua.MustVariant(int32(1))}},
			},
		},
	}
	time.Sleep(30 * time.Millisecond)

	// Second notification's PutValue must still succeed - proving the pump
	// goroutine is still alive and processing after the first failure.
	mgr.notifyCh <- &opcua.PublishNotificationData{
		Value: &ua.DataChangeNotification{
			MonitoredItems: []*ua.MonitoredItemNotification{
				{ClientHandle: 1, Value: &ua.DataValue{Status: ua.StatusOK, Value: ua.MustVariant(int32(7))}},
			},
		},
	}

	waitForCondition(t, func() bool {
		entry, ok, err := cache.GetValue(ctx, "i=1")
		return err == nil && ok && entry.Value == int32(7)
	})
}
