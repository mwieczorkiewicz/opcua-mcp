package opcua

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"pgregory.net/rapid"
)

// okRead answers every ReadRequest with one Good result per requested node.
func okRead(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
	results := make([]*ua.DataValue, len(req.NodesToRead))
	for i := range results {
		results[i] = &ua.DataValue{Status: ua.StatusOK, Value: ua.MustVariant(int32(1))}
	}
	return &ua.ReadResponse{Results: results}, nil
}

// staleRead fails every read with the given stale-session status, wrapped
// the way a caller might see it.
func staleRead(status ua.StatusCode) func(context.Context, *ua.ReadRequest) (*ua.ReadResponse, error) {
	return func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		return nil, fmt.Errorf("send: %w", status)
	}
}

// TestReadRecoversFromStaleSession covers SR-1/2/4: a read that fails with
// either stale-session status reconnects once, closes the old connection,
// and returns the retried read's result.
func TestReadRecoversFromStaleSession(t *testing.T) {
	for _, status := range staleSessionStatuses {
		t.Run(status.Error(), func(t *testing.T) {
			stale := &mockOpcuaClient{readFunc: staleRead(status)}
			fresh := &mockOpcuaClient{readFunc: okRead}
			c, dials := newMockConnectedClient(t, stale, fresh)

			results, err := c.Read(context.Background(), []string{"i=2258"})
			if err != nil {
				t.Fatalf("Read() error: %v", err)
			}
			if len(results) != 1 || results[0].Status != ua.StatusOK {
				t.Fatalf("Read() = %v, want one Good result", results)
			}
			if got := dials.Load(); got != 1 {
				t.Errorf("reconnected %d times, want 1", got)
			}
			if stale.closeCalls != 1 {
				t.Errorf("stale connection closed %d times, want 1", stale.closeCalls)
			}
			if fresh.readCalls != 1 {
				t.Errorf("fresh connection read %d times, want 1", fresh.readCalls)
			}
		})
	}
}

// TestReadRetriesOnlyOnce covers SR-4's "exactly once": if the retry also
// hits a stale session, the error is returned without another reconnect.
func TestReadRetriesOnlyOnce(t *testing.T) {
	first := &mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)}
	second := &mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)}
	c, dials := newMockConnectedClient(t, first, second, &mockOpcuaClient{readFunc: okRead})

	_, err := c.Read(context.Background(), []string{"i=2258"})
	if !errors.Is(err, ua.StatusBadSessionIDInvalid) {
		t.Fatalf("Read() error = %v, want the stale-session status", err)
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("reconnected %d times, want 1", got)
	}
	if first.readCalls+second.readCalls != 2 {
		t.Errorf("issued %d read attempts, want 2", first.readCalls+second.readCalls)
	}
}

// TestNonStaleErrorDoesNotReconnect covers SR-1: only the stale-session
// statuses trigger recovery.
func TestNonStaleErrorDoesNotReconnect(t *testing.T) {
	for _, err := range []error{errors.New("i/o timeout"), ua.StatusBadNodeIDUnknown, ua.StatusBadTimeout} {
		conn := &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) { return nil, err }}
		c, dials := newMockConnectedClient(t, conn)
		if _, gotErr := c.GetNodeClass(context.Background(), "i=85"); gotErr == nil {
			t.Errorf("GetNodeClass() succeeded with %v, want an error", err)
		}
		if got := dials.Load(); got != 0 {
			t.Errorf("%v: reconnected %d times, want 0", err, got)
		}
	}
}

// TestBatchReadStaleSkipsIsolation covers SR-6: a stale batch read must
// recover instead of falling into the per-node isolation fallback, which
// would report the stale session as per-node bad statuses.
func TestBatchReadStaleSkipsIsolation(t *testing.T) {
	stale := &mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)}
	fresh := &mockOpcuaClient{readFunc: okRead}
	c, _ := newMockConnectedClient(t, stale, fresh)

	results, err := c.Read(context.Background(), []string{"i=2258", "i=2257", "i=2259"})
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if stale.readCalls != 1 {
		t.Errorf("stale connection got %d reads, want 1 (no per-node fallback)", stale.readCalls)
	}
	for i, r := range results {
		if r.Status != ua.StatusOK {
			t.Errorf("result %d status %s, want Good", i, r.Status)
		}
	}
}

// TestWriteStaleReconnectsWithoutRetry covers SR-5: a stale write
// reconnects so the next call works, but is not replayed.
func TestWriteStaleReconnectsWithoutRetry(t *testing.T) {
	typeInfoErr := func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		return nil, errors.New("type info unavailable")
	}
	stale := &mockOpcuaClient{
		readFunc: typeInfoErr,
		writeFunc: func(ctx context.Context, req *ua.WriteRequest) (*ua.WriteResponse, error) {
			return nil, ua.StatusBadSessionIDInvalid
		},
	}
	fresh := &mockOpcuaClient{readFunc: typeInfoErr}
	c, dials := newMockConnectedClient(t, stale, fresh)

	err := c.Write(context.Background(), "ns=2;s=Value", int32(5))
	if err == nil || !strings.Contains(err.Error(), "was not applied") {
		t.Fatalf("Write() error = %v, want a 'was not applied' error", err)
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("reconnected %d times, want 1", got)
	}
	if fresh.writeCalls != 0 {
		t.Errorf("write replayed %d times on the new connection, want 0", fresh.writeCalls)
	}
	if !c.IsConnected() {
		t.Error("client not connected after recovery, want the new connection live")
	}
}

// TestDisconnectIsNotRecovered covers SR-8: after a user disconnect,
// recovery for the old generation must not reconnect.
func TestDisconnectIsNotRecovered(t *testing.T) {
	c, dials := newMockConnectedClient(t, &mockOpcuaClient{}, &mockOpcuaClient{})
	gen := c.liveGen()
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error: %v", err)
	}
	if err := c.recoverConnection(context.Background(), gen, "test"); err != nil {
		t.Fatalf("recoverConnection() error = %v, want nil (generation already retired)", err)
	}
	if err := c.recoverConnection(context.Background(), c.liveGen(), "test"); err == nil {
		t.Error("recoverConnection() on a disconnected client succeeded, want an error")
	}
	if got := dials.Load(); got != 0 {
		t.Errorf("reconnected %d times after Disconnect, want 0", got)
	}
	if c.IsConnected() {
		t.Error("client reconnected after a user disconnect")
	}
}

// TestEnsureConnectedOutcomes covers SR-9.
func TestEnsureConnectedOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		connected   bool
		current     *mockOpcuaClient
		next        []*mockOpcuaClient
		wantOutcome ConnectOutcome
		wantDials   int32
		wantErr     bool
	}{
		{
			name:        "healthy connection is kept",
			connected:   true,
			current:     &mockOpcuaClient{readFunc: okRead},
			wantOutcome: ConnectOutcomeAlreadyHealthy,
		},
		{
			name:        "gopcua state not connected is rebuilt",
			connected:   true,
			current:     &mockOpcuaClient{readFunc: okRead, stateFunc: func() opcua.ConnState { return opcua.Disconnected }},
			next:        []*mockOpcuaClient{{readFunc: okRead}},
			wantOutcome: ConnectOutcomeReconnected,
			wantDials:   1,
		},
		{
			name:        "probe rejected by server is rebuilt",
			connected:   true,
			current:     &mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)},
			next:        []*mockOpcuaClient{{readFunc: okRead}},
			wantOutcome: ConnectOutcomeReconnected,
			wantDials:   1,
		},
		{
			name:      "probe returning a bad status is rebuilt",
			connected: true,
			current: &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
				return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusBadSessionClosed}}}, nil
			}},
			next:        []*mockOpcuaClient{{readFunc: okRead}},
			wantOutcome: ConnectOutcomeReconnected,
			wantDials:   1,
		},
		{
			name:        "not connected connects",
			connected:   false,
			current:     &mockOpcuaClient{},
			next:        []*mockOpcuaClient{{readFunc: okRead}},
			wantOutcome: ConnectOutcomeConnected,
			wantDials:   1,
		},
		{
			name:        "rebuild failure is reported",
			connected:   true,
			current:     &mockOpcuaClient{readFunc: staleRead(ua.StatusBadServerNotConnected)},
			wantOutcome: ConnectOutcomeReconnected,
			wantDials:   1,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, dials := newMockConnectedClient(t, tt.current, tt.next...)
			if !tt.connected {
				c.SetConnectedForTesting(false)
			}
			outcome, err := c.EnsureConnected(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnsureConnected() error = %v, wantErr %v", err, tt.wantErr)
			}
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if got := dials.Load(); got != tt.wantDials {
				t.Errorf("dials = %d, want %d", got, tt.wantDials)
			}
			if tt.wantErr && c.IsConnected() {
				t.Error("client reports connected after a failed rebuild")
			}
		})
	}
}

// TestReconnectHandlerRunsAfterRecovery covers SR-7: the registered handler
// (SubscriptionManager.resubscribe in production) runs once per recovery,
// with a context the triggering request's cancellation can't abort.
func TestReconnectHandlerRunsAfterRecovery(t *testing.T) {
	c, _ := newMockConnectedClient(t,
		&mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)},
		&mockOpcuaClient{readFunc: okRead})

	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	var handlerCtxErr error
	c.SetReconnectHandler(func(hctx context.Context) {
		calls++
		cancel()
		handlerCtxErr = hctx.Err()
	})

	if _, err := c.Read(ctx, []string{"i=2258"}); err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if calls != 1 {
		t.Errorf("reconnect handler ran %d times, want 1", calls)
	}
	if handlerCtxErr != nil {
		t.Errorf("handler context was cancelled with the request: %v", handlerCtxErr)
	}
}

// TestSubscriptionManagerResubscribesOnRecovery covers SR-7 end to end at
// the manager level: Start registers the handler, and running it restores
// persisted intent without calling Connect (the client already reconnected).
func TestSubscriptionManagerResubscribesOnRecovery(t *testing.T) {
	client := &mockSubscribingClient{}
	sm := NewSubscriptionManager(client, newTestStoreForSubscription(t), newTestStoreConfig(t))
	if err := sm.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if _, _, err := sm.Subscribe(context.Background(), []string{"ns=2;s=A"}, 1000); err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	if client.reconnectHandler == nil {
		t.Fatal("Start() did not register a reconnect handler")
	}
	connectsBefore, subscribesBefore := client.connectCalls, client.subscribeCalls

	client.reconnectHandler(context.Background())

	if client.connectCalls != connectsBefore {
		t.Errorf("resubscribe called Connect %d time(s), want 0", client.connectCalls-connectsBefore)
	}
	if client.subscribeCalls != subscribesBefore+1 {
		t.Errorf("resubscribe created %d subscription(s), want 1", client.subscribeCalls-subscribesBefore)
	}
	if subs := sm.ListSubscriptions(); len(subs) != 1 {
		t.Errorf("ListSubscriptions() = %d entries after resubscribe, want 1", len(subs))
	}

	if err := sm.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if client.reconnectHandler != nil {
		t.Error("Stop() left the reconnect handler registered")
	}
}

// TestSessionRecoveryRetriesAtMostOnce (PBT-03 invariant) drives random
// sequences of reads whose attempts succeed, fail with a stale session, or
// fail otherwise, and checks against a model: each op makes at most two
// attempts, reconnects exactly when its first attempt was stale, and
// succeeds exactly when its last attempt succeeded.
func TestSessionRecoveryRetriesAtMostOnce(t *testing.T) {
	type outcome int
	const (
		ok outcome = iota
		stale
		other
	)

	rapid.Check(t, func(rt *rapid.T) {
		nOps := rapid.IntRange(1, 20).Draw(rt, "ops")
		var script []outcome
		var mu sync.Mutex
		attempts := 0
		read := func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			o := script[attempts]
			attempts++
			switch o {
			case stale:
				return nil, ua.StatusBadSessionIDInvalid
			case other:
				return nil, errors.New("decode failure")
			}
			return okRead(ctx, req)
		}

		c := newTestClient(t)
		var dials int
		c.dial = func(ctx context.Context, gen uint64) (opcuaClient, error) {
			dials++
			return &mockOpcuaClient{readFunc: read}, nil
		}
		c.mu.Lock()
		c.client = &mockOpcuaClient{readFunc: read}
		c.connected = true
		c.mu.Unlock()

		for i := 0; i < nOps; i++ {
			first := outcome(rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("op%d.first", i)))
			ops := []outcome{first}
			if first == stale {
				ops = append(ops, outcome(rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("op%d.retry", i))))
			}
			script = append(script, ops...)
			attemptsBefore, dialsBefore := attempts, dials

			_, err := c.Read(context.Background(), []string{"i=2258"})

			if got := attempts - attemptsBefore; got != len(ops) {
				rt.Fatalf("op %d made %d attempts, model says %d (%v)", i, got, len(ops), ops)
			}
			wantDials := 0
			if first == stale {
				wantDials = 1
			}
			if got := dials - dialsBefore; got != wantDials {
				rt.Fatalf("op %d reconnected %d times, model says %d (%v)", i, got, wantDials, ops)
			}
			if wantOK := ops[len(ops)-1] == ok; (err == nil) != wantOK {
				rt.Fatalf("op %d err = %v, model says success=%v (%v)", i, err, wantOK, ops)
			}
		}
	})
}

// TestRecoverIsIdempotentPerGeneration (PBT-04) checks that any number of
// concurrent callers failing on the same stale connection cause exactly one
// reconnect, and all of them end up succeeding on the new connection.
func TestRecoverIsIdempotentPerGeneration(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 16).Draw(rt, "callers")
		stale := &mockOpcuaClient{readFunc: staleRead(ua.StatusBadSessionIDInvalid)}
		fresh := &mockOpcuaClient{readFunc: okRead}
		c, dials := newMockConnectedClient(t, stale, fresh)

		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = c.Read(context.Background(), []string{"i=2258"})
			}(i)
		}
		wg.Wait()

		if got := dials.Load(); got != 1 {
			rt.Fatalf("%d concurrent callers caused %d reconnects, want 1", n, got)
		}
		for i, err := range errs {
			if err != nil {
				rt.Fatalf("caller %d failed: %v", i, err)
			}
		}
	})
}

// TestIsStaleSessionErrorWrapDepth (PBT-05 oracle) checks detection at any
// wrap depth against a plain membership check.
func TestIsStaleSessionErrorWrapDepth(t *testing.T) {
	candidates := append([]ua.StatusCode{ua.StatusBadNodeIDUnknown, ua.StatusBadTimeout, ua.StatusBadSessionClosed, ua.StatusOK}, staleSessionStatuses...)
	rapid.Check(t, func(rt *rapid.T) {
		status := rapid.SampledFrom(candidates).Draw(rt, "status")
		depth := rapid.IntRange(0, 6).Draw(rt, "depth")

		var err error = status
		for i := 0; i < depth; i++ {
			err = fmt.Errorf("layer %d: %w", i, err)
		}

		want := false
		for _, s := range staleSessionStatuses {
			if s == status {
				want = true
			}
		}
		if got := isStaleSessionError(err); got != want {
			rt.Fatalf("isStaleSessionError(%v) = %v, want %v", err, got, want)
		}
	})
}
