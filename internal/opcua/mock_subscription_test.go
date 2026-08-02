package opcua

import (
	"context"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// mockSubscribingClient implements subscribingClient for tests.
type mockSubscribingClient struct {
	connectFunc   func(ctx context.Context) error
	subscribeFunc func(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error)

	connectCalls   int
	subscribeCalls int
	stateCh        chan<- opcua.ConnState
}

func (m *mockSubscribingClient) Connect(ctx context.Context) error {
	m.connectCalls++
	if m.connectFunc != nil {
		return m.connectFunc(ctx)
	}
	return nil
}

func (m *mockSubscribingClient) SetStateChangeChannel(ch chan<- opcua.ConnState) {
	m.stateCh = ch
}

func (m *mockSubscribingClient) Subscribe(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (subscriptionHandle, error) {
	m.subscribeCalls++
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, params, notifyCh)
	}
	return &mockSubscriptionHandle{}, nil
}

// mockSubscriptionHandle implements subscriptionHandle for tests.
type mockSubscriptionHandle struct {
	monitorFunc   func(ctx context.Context, ts ua.TimestampsToReturn, items ...*ua.MonitoredItemCreateRequest) (*ua.CreateMonitoredItemsResponse, error)
	unmonitorFunc func(ctx context.Context, monitoredItemIDs ...uint32) (*ua.DeleteMonitoredItemsResponse, error)
	cancelFunc    func(ctx context.Context) error

	monitorCalls   int
	unmonitorCalls int
	cancelCalls    int
}

func (m *mockSubscriptionHandle) Monitor(ctx context.Context, ts ua.TimestampsToReturn, items ...*ua.MonitoredItemCreateRequest) (*ua.CreateMonitoredItemsResponse, error) {
	m.monitorCalls++
	if m.monitorFunc != nil {
		return m.monitorFunc(ctx, ts, items...)
	}
	// Default: accept every item, assigning sequential MonitoredItemIDs.
	results := make([]*ua.MonitoredItemCreateResult, len(items))
	for i := range items {
		results[i] = &ua.MonitoredItemCreateResult{StatusCode: ua.StatusOK, MonitoredItemID: uint32(i + 1)} //nolint:gosec // test-only, small bounded index
	}
	return &ua.CreateMonitoredItemsResponse{Results: results}, nil
}

func (m *mockSubscriptionHandle) Unmonitor(ctx context.Context, monitoredItemIDs ...uint32) (*ua.DeleteMonitoredItemsResponse, error) {
	m.unmonitorCalls++
	if m.unmonitorFunc != nil {
		return m.unmonitorFunc(ctx, monitoredItemIDs...)
	}
	results := make([]ua.StatusCode, len(monitoredItemIDs))
	for i := range results {
		results[i] = ua.StatusOK
	}
	return &ua.DeleteMonitoredItemsResponse{Results: results}, nil
}

func (m *mockSubscriptionHandle) Cancel(ctx context.Context) error {
	m.cancelCalls++
	if m.cancelFunc != nil {
		return m.cancelFunc(ctx)
	}
	return nil
}

// acceptAllMonitorResults builds a CreateMonitoredItemsResponse accepting
// every item, assigning MonitoredItemIDs starting at startID.
func acceptAllMonitorResults(items []*ua.MonitoredItemCreateRequest, startID uint32) *ua.CreateMonitoredItemsResponse {
	results := make([]*ua.MonitoredItemCreateResult, len(items))
	for i := range items {
		results[i] = &ua.MonitoredItemCreateResult{StatusCode: ua.StatusOK, MonitoredItemID: startID + uint32(i)}
	}
	return &ua.CreateMonitoredItemsResponse{Results: results}
}
