package opcua

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopcua/opcua"
)

func TestReconnectWatcherFiresOnceOnPermanentDeath(t *testing.T) {
	client := &mockSubscribingClient{}
	var fired int32
	watcher := NewReconnectWatcher(client, func(ctx context.Context) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer watcher.Stop()

	if client.stateCh == nil {
		t.Fatal("Start() did not register a state channel via SetStateChangeChannel")
	}

	client.stateCh <- opcua.Connected
	client.stateCh <- opcua.Closed

	waitForCondition(t, func() bool { return atomic.LoadInt32(&fired) == 1 })
}

func TestReconnectWatcherIgnoresTransientStates(t *testing.T) {
	client := &mockSubscribingClient{}
	var fired int32
	watcher := NewReconnectWatcher(client, func(ctx context.Context) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer watcher.Stop()

	client.stateCh <- opcua.Connected
	client.stateCh <- opcua.Reconnecting
	client.stateCh <- opcua.Connected
	client.stateCh <- opcua.Disconnected
	client.stateCh <- opcua.Connected

	// Give the watcher goroutine a moment to process all of the above before
	// asserting the negative (never fired).
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("onPermanentDeath fired %d times for transient states, want 0", got)
	}
}

func TestReconnectWatcherIgnoresClosedWithoutPriorConnected(t *testing.T) {
	client := &mockSubscribingClient{}
	var fired int32
	watcher := NewReconnectWatcher(client, func(ctx context.Context) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer watcher.Stop()

	// Closed without ever having observed Connected first (e.g. the initial
	// connection attempt itself failed) must not fire (BR-8).
	client.stateCh <- opcua.Closed

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("onPermanentDeath fired %d times for Closed with no prior Connected, want 0", got)
	}
}

func TestReconnectWatcherResetsAfterRebuild(t *testing.T) {
	client := &mockSubscribingClient{}
	var fired int32
	watcher := NewReconnectWatcher(client, func(ctx context.Context) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer watcher.Stop()

	// First death.
	client.stateCh <- opcua.Connected
	client.stateCh <- opcua.Closed
	waitForCondition(t, func() bool { return atomic.LoadInt32(&fired) == 1 })

	// A second, independent lifetime: Connected then Closed again must fire again.
	client.stateCh <- opcua.Connected
	client.stateCh <- opcua.Closed
	waitForCondition(t, func() bool { return atomic.LoadInt32(&fired) == 2 })
}

// waitForCondition polls cond until it's true or fails the test after a
// short timeout - used instead of a fixed sleep since the watcher processes
// channel sends asynchronously.
func waitForCondition(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
