package opcua

import (
	"context"
	"sync"

	"github.com/gopcua/opcua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// ReconnectWatcher watches an OPC-UA client's connection state for a
// permanent death (a transition to Closed after having been Connected) and
// invokes a rebuild callback when that happens. It deliberately does NOT
// react to Connecting/Reconnecting/transient Disconnected states - gopcua's
// own AutoReconnect machinery already keeps existing subscriptions working
// across those (verified against gopcua v0.9.0 source during Phase 2
// planning); only the unrecoverable case needs an application-level
// response.
type ReconnectWatcher struct {
	client           subscribingClient
	onPermanentDeath func(ctx context.Context) error

	stateCh chan opcua.ConnState
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewReconnectWatcher creates a watcher that calls onPermanentDeath exactly
// once per detected permanent death of client's connection.
func NewReconnectWatcher(client subscribingClient, onPermanentDeath func(ctx context.Context) error) *ReconnectWatcher {
	return &ReconnectWatcher{
		client:           client,
		onPermanentDeath: onPermanentDeath,
		stateCh:          make(chan opcua.ConnState, 16),
		stopCh:           make(chan struct{}),
	}
}

// Start registers the watcher's channel with client and begins watching.
func (w *ReconnectWatcher) Start(ctx context.Context) error {
	w.client.SetStateChangeChannel(w.stateCh)

	w.wg.Add(1)
	go w.watch(ctx)
	return nil
}

// Stop stops the watcher and waits for its goroutine to exit.
func (w *ReconnectWatcher) Stop() error {
	close(w.stopCh)
	w.wg.Wait()
	return nil
}

func (w *ReconnectWatcher) watch(ctx context.Context) {
	defer w.wg.Done()

	everConnected := false
	for {
		select {
		case state := <-w.stateCh:
			switch {
			case state == opcua.Connected:
				everConnected = true
			case state == opcua.Closed && everConnected:
				logger.Warn("OPC-UA client permanently closed, rebuilding connection and resubscribing")
				if err := w.onPermanentDeath(ctx); err != nil {
					logger.Error("failed to rebuild OPC-UA connection after permanent death", "error", err)
				}
				// Reset: the rebuild (if successful) starts a new Client
				// lifetime, which must observe its own Connected before
				// this watcher reacts to a future Closed again.
				everConnected = false
			}
		case <-w.stopCh:
			return
		}
	}
}
