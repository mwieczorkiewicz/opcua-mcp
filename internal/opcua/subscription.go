package opcua

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

// subscribingClient is the subset of *Client's surface SubscriptionManager
// and ReconnectWatcher need, mirroring the opcuaClient mock-seam pattern.
type subscribingClient interface {
	Connect(ctx context.Context) error
	SetStateChangeChannel(ch chan<- opcua.ConnState)
	Subscribe(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error)
}

var _ subscribingClient = (*Client)(nil)

// subscriptionHandle is the subset of *opcua.Subscription's surface this
// package needs. gopcua's concrete *opcua.Subscription satisfies it
// implicitly (its real method signatures match exactly) - this interface
// exists purely so tests can inject a mock, since the concrete type has
// unexported fields and can't be faked directly.
type subscriptionHandle interface {
	Monitor(ctx context.Context, ts ua.TimestampsToReturn, items ...*ua.MonitoredItemCreateRequest) (*ua.CreateMonitoredItemsResponse, error)
	Unmonitor(ctx context.Context, monitoredItemIDs ...uint32) (*ua.DeleteMonitoredItemsResponse, error)
	Cancel(ctx context.Context) error
}

var _ subscriptionHandle = (*opcua.Subscription)(nil)

// subscriptionStore is the narrow slice of store.Store's surface
// SubscriptionManager needs.
type subscriptionStore interface {
	PutValue(ctx context.Context, nodeID string, entry store.ValueEntry) error
	PutSubscription(ctx context.Context, id string, intent store.SubscriptionIntent) error
	DeleteSubscription(ctx context.Context, id string) error
	ListSubscriptions(ctx context.Context) ([]store.SubscriptionIntent, error)
}

// NodeStatus reports a single node's outcome within a Subscribe call.
type NodeStatus struct {
	NodeID string
	Status string
}

// SubscriptionInfo is the tool-facing shape returned by ListSubscriptions.
type SubscriptionInfo struct {
	ID         string
	NodeIDs    []string
	IntervalMs int
	CreatedAt  time.Time
}

// subscriptionRecord is the in-memory record of a logical subscription.
// The persisted form is store.SubscriptionIntent.
type subscriptionRecord struct {
	ID         string
	NodeIDs    []string // only nodes the server actually accepted (BR-4)
	IntervalMs int
	CreatedAt  time.Time
}

// intervalGroup is one gopcua Subscription shared by every logical
// subscription requesting IntervalMs (BR-2).
type intervalGroup struct {
	IntervalMs              int
	Sub                     subscriptionHandle
	RefCount                int
	ClientHandleToNodeID    map[uint32]string // routes incoming notifications (keyed by the client-assigned handle)
	NodeIDToClientHandle    map[string]uint32 // reverse, for removing routing entries on Unsubscribe
	NodeIDToMonitoredItemID map[string]uint32 // server-assigned IDs, required by Unmonitor
}

// SubscriptionManager manages the lifecycle of OPC-UA subscriptions: create,
// cancel, list, persist intent, and batch incoming notifications into the
// values cache. See aidlc-docs/construction/unit-2-subscription-management/
// for the full design.
type SubscriptionManager struct {
	client  subscribingClient
	cache   subscriptionStore
	watcher *ReconnectWatcher
	cfg     *config.StoreConfig

	mu             sync.RWMutex
	subscriptions  map[string]*subscriptionRecord
	intervalGroups map[int]*intervalGroup
	nextHandle     uint32

	notifyCh chan *opcua.PublishNotificationData
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewSubscriptionManager creates a SubscriptionManager. It constructs its
// own ReconnectWatcher internally (rather than taking one as a parameter),
// since the watcher's rebuild callback must reference the manager itself -
// a refinement over application-design.md's original sketch, noted in the
// code generation summary.
func NewSubscriptionManager(client subscribingClient, cache subscriptionStore, cfg *config.StoreConfig) *SubscriptionManager {
	s := &SubscriptionManager{
		client:         client,
		cache:          cache,
		cfg:            cfg,
		subscriptions:  make(map[string]*subscriptionRecord),
		intervalGroups: make(map[int]*intervalGroup),
		notifyCh:       make(chan *opcua.PublishNotificationData, cfg.NotifyChanBuffer),
		stopCh:         make(chan struct{}),
	}
	s.watcher = NewReconnectWatcher(client, s.rebuild)
	return s
}

// Start performs the warm-start (BR-7): persisted subscriptions are
// restored before Start returns, so ListSubscriptions is always consistent
// with persisted intent by the time any caller can reach it. It then starts
// the reconnect watcher and the notification pump.
func (s *SubscriptionManager) Start(ctx context.Context) error {
	if err := s.warmStart(ctx); err != nil {
		return fmt.Errorf("subscription manager warm start: %w", err)
	}
	if err := s.watcher.Start(ctx); err != nil {
		return fmt.Errorf("start reconnect watcher: %w", err)
	}

	s.wg.Add(1)
	go s.pump(ctx)
	return nil
}

// Stop stops the notification pump and the reconnect watcher, joining both.
func (s *SubscriptionManager) Stop() error {
	close(s.stopCh)
	s.wg.Wait()
	return s.watcher.Stop()
}

// warmStart restores every persisted subscription intent. A single
// restoration failure is logged and skipped rather than aborting the rest
// (findings.md's discovery-error-swallowing critique doesn't apply here:
// this failure IS surfaced, via logging, just not treated as fatal to the
// remaining restorations).
func (s *SubscriptionManager) warmStart(ctx context.Context) error {
	intents, err := s.cache.ListSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("load persisted subscriptions: %w", err)
	}

	for _, intent := range intents {
		if _, _, err := s.subscribeInternal(ctx, intent.ID, intent.NodeIDs, intent.IntervalMs, intent.CreatedAt); err != nil {
			logger.Error("failed to restore subscription", "subscription_id", intent.ID, "error", err)
		}
	}
	return nil
}

// rebuild is ReconnectWatcher's onPermanentDeath callback (BR-5,
// synchronous): reconnect, discard the dead client's in-memory subscription
// state (its gopcua Subscriptions died with it), and restore everything
// from persisted intent again.
func (s *SubscriptionManager) rebuild(ctx context.Context) error {
	if err := s.client.Connect(ctx); err != nil {
		return fmt.Errorf("reconnect: %w", err)
	}

	s.mu.Lock()
	s.subscriptions = make(map[string]*subscriptionRecord)
	s.intervalGroups = make(map[int]*intervalGroup)
	s.mu.Unlock()

	return s.warmStart(ctx)
}

// Subscribe creates a new logical subscription for nodeIDs at intervalMs.
// Per BR-4, it succeeds partially: nodes the server rejects are reported in
// rejected, never silently dropped, and don't fail the whole call unless
// every requested node was rejected.
func (s *SubscriptionManager) Subscribe(ctx context.Context, nodeIDs []string, intervalMs int) (id string, rejected []NodeStatus, err error) {
	return s.subscribeInternal(ctx, uuid.NewString(), nodeIDs, intervalMs, time.Now().UTC())
}

func (s *SubscriptionManager) subscribeInternal(ctx context.Context, id string, nodeIDs []string, intervalMs int, createdAt time.Time) (string, []NodeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.intervalGroups[intervalMs]
	if !ok {
		params := &opcua.SubscriptionParameters{Interval: time.Duration(intervalMs) * time.Millisecond}
		sub, err := s.client.Subscribe(ctx, params, s.notifyCh)
		if err != nil {
			return "", nil, fmt.Errorf("create subscription at interval %dms: %w", intervalMs, err)
		}
		group = &intervalGroup{
			IntervalMs:              intervalMs,
			Sub:                     sub,
			ClientHandleToNodeID:    make(map[uint32]string),
			NodeIDToClientHandle:    make(map[string]uint32),
			NodeIDToMonitoredItemID: make(map[string]uint32),
		}
		s.intervalGroups[intervalMs] = group
	}

	requests := make([]*ua.MonitoredItemCreateRequest, 0, len(nodeIDs))
	requestNodeIDs := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		parsed, err := ua.ParseNodeID(nodeID)
		if err != nil {
			return "", nil, fmt.Errorf("invalid node ID %s: %w", nodeID, err)
		}
		s.nextHandle++
		requests = append(requests, opcua.NewMonitoredItemCreateRequestWithDefaults(parsed, ua.AttributeIDValue, s.nextHandle))
		requestNodeIDs = append(requestNodeIDs, nodeID)
	}

	resp, err := group.Sub.Monitor(ctx, ua.TimestampsToReturnBoth, requests...)
	if err != nil {
		return "", nil, fmt.Errorf("monitor nodes: %w", err)
	}

	var accepted []string
	var rejected []NodeStatus
	for i, result := range resp.Results {
		nodeID := requestNodeIDs[i]
		handle := requests[i].RequestedParameters.ClientHandle
		if result.StatusCode != ua.StatusOK {
			rejected = append(rejected, NodeStatus{NodeID: nodeID, Status: result.StatusCode.Error()})
			continue
		}
		group.ClientHandleToNodeID[handle] = nodeID
		group.NodeIDToClientHandle[nodeID] = handle
		group.NodeIDToMonitoredItemID[nodeID] = result.MonitoredItemID
		accepted = append(accepted, nodeID)
	}

	if len(accepted) == 0 {
		if group.RefCount == 0 {
			_ = group.Sub.Cancel(ctx)
			delete(s.intervalGroups, intervalMs)
		}
		return "", rejected, fmt.Errorf("no requested nodes were accepted")
	}

	record := &subscriptionRecord{ID: id, NodeIDs: accepted, IntervalMs: intervalMs, CreatedAt: createdAt}
	s.subscriptions[id] = record
	group.RefCount++

	intent := store.SubscriptionIntent{ID: id, NodeIDs: accepted, IntervalMs: intervalMs, CreatedAt: createdAt}
	if err := s.cache.PutSubscription(ctx, id, intent); err != nil {
		return id, rejected, fmt.Errorf("persist subscription %s: %w", id, err)
	}

	return id, rejected, nil
}

// Unsubscribe removes every node in the logical subscription id at once
// (BR-3, whole-group only).
func (s *SubscriptionManager) Unsubscribe(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.subscriptions[id]
	if !ok {
		return fmt.Errorf("subscription %s not found", id)
	}

	if group, ok := s.intervalGroups[record.IntervalMs]; ok {
		var monitoredItemIDs []uint32
		for _, nodeID := range record.NodeIDs {
			if miID, ok := group.NodeIDToMonitoredItemID[nodeID]; ok {
				monitoredItemIDs = append(monitoredItemIDs, miID)
			}
			if handle, ok := group.NodeIDToClientHandle[nodeID]; ok {
				delete(group.ClientHandleToNodeID, handle)
			}
			delete(group.NodeIDToClientHandle, nodeID)
			delete(group.NodeIDToMonitoredItemID, nodeID)
		}
		if len(monitoredItemIDs) > 0 {
			if _, err := group.Sub.Unmonitor(ctx, monitoredItemIDs...); err != nil {
				logger.Warn("failed to unmonitor nodes", "subscription_id", id, "error", err)
			}
		}

		group.RefCount--
		if group.RefCount <= 0 {
			if err := group.Sub.Cancel(ctx); err != nil {
				logger.Warn("failed to cancel gopcua subscription", "interval_ms", record.IntervalMs, "error", err)
			}
			delete(s.intervalGroups, record.IntervalMs)
		}
	}

	delete(s.subscriptions, id)
	if err := s.cache.DeleteSubscription(ctx, id); err != nil {
		return fmt.Errorf("delete persisted subscription %s: %w", id, err)
	}
	return nil
}

// ListSubscriptions returns every active logical subscription. Reads
// in-memory state (safe per BR-7's warm-start-before-Start-returns
// guarantee) rather than the store directly - this can't fail, so unlike
// store.ListSubscriptions it returns no error.
func (s *SubscriptionManager) ListSubscriptions() []SubscriptionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]SubscriptionInfo, 0, len(s.subscriptions))
	for _, r := range s.subscriptions {
		out = append(out, SubscriptionInfo{
			ID:         r.ID,
			NodeIDs:    append([]string(nil), r.NodeIDs...),
			IntervalMs: r.IntervalMs,
			CreatedAt:  r.CreatedAt,
		})
	}
	return out
}

// pump batches incoming notifications into values-bucket writes within
// cfg.BatchWindow/BatchMaxItems.
func (s *SubscriptionManager) pump(ctx context.Context) {
	defer s.wg.Done()

	batch := make(map[string]store.ValueEntry)
	ticker := time.NewTicker(s.cfg.BatchWindow)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		for nodeID, entry := range batch {
			if err := s.cache.PutValue(ctx, nodeID, entry); err != nil {
				logger.Warn("failed to cache subscription value", "node_id", nodeID, "error", err) // BR-6: log and continue
			}
		}
		batch = make(map[string]store.ValueEntry)
	}

	for {
		select {
		case notif := <-s.notifyCh:
			s.handleNotification(notif, batch)
			if len(batch) >= s.cfg.BatchMaxItems {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.stopCh:
			flush()
			return
		}
	}
}

func (s *SubscriptionManager) handleNotification(notif *opcua.PublishNotificationData, batch map[string]store.ValueEntry) {
	if notif.Error != nil {
		logger.Warn("subscription notification error", "error", notif.Error)
		return
	}

	dcn, ok := notif.Value.(*ua.DataChangeNotification)
	if !ok {
		return // event/status-change notifications are out of this unit's scope (Value-attribute monitoring only)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, item := range dcn.MonitoredItems {
		nodeID, found := s.lookupNodeIDLocked(item.ClientHandle)
		if !found || item.Value == nil {
			continue
		}
		batch[nodeID] = store.ValueEntry{
			Value:           item.Value.Value.Value(),
			Status:          item.Value.Status.Error(),
			SourceTimestamp: item.Value.SourceTimestamp,
			ServerTimestamp: item.Value.ServerTimestamp,
			ReceivedAt:      time.Now().UTC(),
			Source:          "subscription",
		}
	}
}

// lookupNodeIDLocked searches every intervalGroup for handle - notifyCh is
// shared across all of them, so a notification's SubscriptionID alone
// doesn't tell us which intervalGroup's map to check without an additional
// gopcua-SubscriptionID-to-intervalGroup index; a linear scan is simple and
// fine at this project's confirmed scale (few distinct intervals). Caller
// must hold mu (read or write).
func (s *SubscriptionManager) lookupNodeIDLocked(handle uint32) (string, bool) {
	for _, group := range s.intervalGroups {
		if nodeID, ok := group.ClientHandleToNodeID[handle]; ok {
			return nodeID, true
		}
	}
	return "", false
}
