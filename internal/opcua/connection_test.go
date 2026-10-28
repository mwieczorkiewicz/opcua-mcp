package opcua

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gopcua/opcua"
)

// newMockConnectedClient returns a Client connected to initial, whose
// reconnects hand out next in order (and fail once next runs out), plus a
// counter of how many dials happened.
func newMockConnectedClient(t *testing.T, initial *mockOpcuaClient, next ...*mockOpcuaClient) (*Client, *atomic.Int32) {
	t.Helper()
	c := newTestClient(t)
	var dials atomic.Int32
	var mu sync.Mutex
	c.dial = func(ctx context.Context, gen uint64) (opcuaClient, error) {
		dials.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if len(next) == 0 {
			return nil, errors.New("dial: no server")
		}
		conn := next[0]
		next = next[1:]
		return conn, nil
	}
	c.mu.Lock()
	c.client = initial
	c.connected = true
	c.clientGen = c.connGen.Add(1)
	c.mu.Unlock()
	return c, &dials
}

// TestStateForwarderDropsRetiredGeneration covers the disconnect fix: once
// a connection is retired, the Closed state its Close() emits must not
// reach the ReconnectWatcher, or it would reconnect after every deliberate
// disconnect.
func TestStateForwarderDropsRetiredGeneration(t *testing.T) {
	c, _ := newMockConnectedClient(t, &mockOpcuaClient{})
	ch := make(chan opcua.ConnState, 4)
	forward := c.stateForwarder(c.connGen.Load(), ch)

	forward(opcua.Connected)
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error: %v", err)
	}
	forward(opcua.Closed)

	close(ch)
	var got []opcua.ConnState
	for s := range ch {
		got = append(got, s)
	}
	if len(got) != 1 || got[0] != opcua.Connected {
		t.Errorf("forwarded states %v, want only [Connected]", got)
	}
}

// TestReconnectSwapsAfterDial covers connectLocked: the old connection is
// closed only after the new one is ready, and a failed dial leaves the
// client disconnected rather than on the old connection.
func TestReconnectSwapsAfterDial(t *testing.T) {
	tests := []struct {
		name          string
		next          []*mockOpcuaClient
		wantErr       bool
		wantConnected bool
	}{
		{name: "successful reconnect", next: []*mockOpcuaClient{{}}, wantConnected: true},
		{name: "failed reconnect", wantErr: true, wantConnected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := &mockOpcuaClient{}
			c, dials := newMockConnectedClient(t, old, tt.next...)
			err := c.Connect(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Connect() error = %v, wantErr %v", err, tt.wantErr)
			}
			if dials.Load() != 1 {
				t.Errorf("dials = %d, want 1", dials.Load())
			}
			if old.closeCalls != 1 {
				t.Errorf("old connection closed %d times, want 1", old.closeCalls)
			}
			if c.IsConnected() != tt.wantConnected {
				t.Errorf("IsConnected() = %v, want %v", c.IsConnected(), tt.wantConnected)
			}
		})
	}
}
