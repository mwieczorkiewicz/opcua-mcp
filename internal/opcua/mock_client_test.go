package opcua

import (
	"context"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// mockOpcuaClient implements opcuaClient for tests, returning canned
// responses via injectable funcs and counting calls so behavior (e.g. "no
// write RPC attempted") can be asserted without a live OPC-UA server.
type mockOpcuaClient struct {
	readFunc       func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error)
	writeFunc      func(ctx context.Context, req *ua.WriteRequest) (*ua.WriteResponse, error)
	browseFunc     func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error)
	browseNextFunc func(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error)
	subscribeFunc  func(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (*opcua.Subscription, error)

	readCalls       int
	writeCalls      int
	browseCalls     int
	browseNextCalls int
	subscribeCalls  int
}

func (m *mockOpcuaClient) Connect(ctx context.Context) error { return nil }
func (m *mockOpcuaClient) Close(ctx context.Context) error   { return nil }

func (m *mockOpcuaClient) Read(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
	m.readCalls++
	if m.readFunc != nil {
		return m.readFunc(ctx, req)
	}
	return &ua.ReadResponse{}, nil
}

func (m *mockOpcuaClient) Write(ctx context.Context, req *ua.WriteRequest) (*ua.WriteResponse, error) {
	m.writeCalls++
	if m.writeFunc != nil {
		return m.writeFunc(ctx, req)
	}
	return &ua.WriteResponse{Results: []ua.StatusCode{ua.StatusOK}}, nil
}

func (m *mockOpcuaClient) Browse(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
	m.browseCalls++
	if m.browseFunc != nil {
		return m.browseFunc(ctx, req)
	}
	return &ua.BrowseResponse{}, nil
}

func (m *mockOpcuaClient) BrowseNext(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error) {
	m.browseNextCalls++
	if m.browseNextFunc != nil {
		return m.browseNextFunc(ctx, req)
	}
	return &ua.BrowseNextResponse{}, nil
}

// Subscribe exists only to satisfy opcuaClient - Client.client's low-level
// interface needs it to match *opcua.Client's real signature exactly (see
// client.go), but no test drives subscriptions through this seam: gopcua's
// concrete *opcua.Subscription can't be faked here (unexported fields), so
// SubscriptionManager's own tests mock the separate, purpose-built
// subscribingClient/SubscriptionHandle interfaces in subscription.go instead.
func (m *mockOpcuaClient) Subscribe(ctx context.Context, params *opcua.SubscriptionParameters, notifyCh chan<- *opcua.PublishNotificationData) (*opcua.Subscription, error) {
	m.subscribeCalls++
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, params, notifyCh)
	}
	return nil, nil
}

// singleAttrReadResponse builds a ReadResponse with one OK result carrying
// the given value, for stubbing the single-attribute ReadRequests that
// GetNodeDataType/GetNodeValueRank/GetNodeArrayDimensions/GetNodeAccessLevel/
// GetNodeUserAccessLevel each issue.
func singleAttrReadResponse(value interface{}) *ua.ReadResponse {
	return &ua.ReadResponse{
		Results: []*ua.DataValue{
			{Status: ua.StatusOK, Value: ua.MustVariant(value)},
		},
	}
}

// typeInfoReadFunc returns a Read stub that answers GetNodeTypeInfo's 5
// sequential single-attribute reads for a writable, scalar node of the given
// data type.
func typeInfoReadFunc(dataType *ua.NodeID, accessLevel byte) func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
	return func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		attr := req.NodesToRead[0].AttributeID
		switch attr {
		case ua.AttributeIDDataType:
			return singleAttrReadResponse(dataType), nil
		case ua.AttributeIDValueRank:
			return singleAttrReadResponse(int32(-1)), nil // scalar
		case ua.AttributeIDArrayDimensions:
			// Deliberately the wrong type so the []uint32 assertion in
			// GetNodeArrayDimensions fails and it falls back to nil, nil -
			// mirroring a scalar node with no ArrayDimensions attribute.
			return singleAttrReadResponse(byte(0)), nil
		case ua.AttributeIDAccessLevel:
			return singleAttrReadResponse(accessLevel), nil
		case ua.AttributeIDUserAccessLevel:
			return singleAttrReadResponse(accessLevel), nil
		default:
			return nil, ua.StatusBadAttributeIDInvalid
		}
	}
}
