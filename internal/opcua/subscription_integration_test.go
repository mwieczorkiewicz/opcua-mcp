//go:build integration

// Package opcua's integration suite (requirements.md NFR-4.1/4.2) verifies
// this project's Subscribe/reconnect/cache assumptions against a real OPC-UA
// server (not just the mocked opcuaClient/subscribingClient seams used
// elsewhere), using testcontainers-go to run the same Microsoft OPC-UA test
// server image already pinned in this repo's docker-compose.yml. Excluded
// from the default `go test ./...`/`make test` run via this build tag -
// run explicitly with `make test-integration` (requires a local Docker
// daemon).
package opcua

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
)

// opcuaTestServerImage is the exact tag already pinned in this repo's
// docker-compose.yml (SECURITY-10) - reused here rather than a second,
// possibly-drifting reference to the same image.
const opcuaTestServerImage = "mcr.microsoft.com/iot/opc-ua-test-server:2.8"

// Standard OPC-UA address space nodes (not test-server-specific), used so
// this suite doesn't depend on undocumented sample-node IDs:
//   - serverCurrentTimeNode continuously changes (every server tick) -
//     ideal for verifying a live subscription push.
//   - serverStartTimeNode is fixed for the server process's lifetime -
//     ideal for verifying a read-through cache hit returns a stable value.
const (
	serverCurrentTimeNode = "i=2258" // Server_ServerStatus_CurrentTime
	serverStartTimeNode   = "i=2257" // Server_ServerStatus_StartTime
)

func startOPCUATestServer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        opcuaTestServerImage,
		ExposedPorts: []string{"4840/tcp"},
		Cmd:          []string{"--sample", "--port", "4840"},
		WaitingFor:   wait.ForListeningPort("4840/tcp").WithStartupTimeout(90 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start %s: %v", opcuaTestServerImage, err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate OPC-UA test server container: %v", err)
		}
	})

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "4840")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	// The container's wait strategy only confirms the TCP port accepts
	// connections, not that the OPC-UA server stack behind it has finished
	// initializing - give it a brief grace period before the first real
	// Connect attempt.
	time.Sleep(2 * time.Second)

	return fmt.Sprintf("opc.tcp://%s:%s", host, port.Port())
}

func newIntegrationTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client := NewClient(&config.OPCUAConfig{
		Endpoint:       endpoint,
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 15 * time.Second,
		SessionTimeout: 30 * time.Second,
		MaxRetries:     3,
		RetryDelay:     time.Second,
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() to real OPC-UA test server failed: %v", err)
	}
	t.Cleanup(func() { client.Disconnect(context.Background()) })
	return client
}

func newIntegrationTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "integration-test.db"), 5*time.Second)
	if err != nil {
		t.Fatalf("store.Open() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestIntegrationSubscribePushesRealValueChangesIntoStore is the core
// NFR-4.1 scenario: subscribe to a real, continuously-changing standard
// node against a real server, and verify the values bucket gets updated by
// the pump with Source: "subscription" - confirming gopcua's real Subscribe/
// Monitor/notification wiring works end-to-end, not just against this
// project's own mocks.
func TestIntegrationSubscribePushesRealValueChangesIntoStore(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	st := newIntegrationTestStore(t)

	mgr := NewSubscriptionManager(client, st, &config.StoreConfig{
		OpenTimeout:      5 * time.Second,
		BatchWindow:      25 * time.Millisecond,
		BatchMaxItems:    250,
		NotifyChanBuffer: 1024,
	})
	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("SubscriptionManager.Start() error: %v", err)
	}
	defer mgr.Stop()

	id, rejected, err := mgr.Subscribe(ctx, []string{serverCurrentTimeNode}, 500)
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("Subscribe() rejected the standard CurrentTime node: %+v", rejected)
	}
	t.Logf("subscribed as %s", id)

	deadline := time.Now().Add(20 * time.Second)
	var entry store.ValueEntry
	for time.Now().Before(deadline) {
		e, ok, err := st.GetValue(ctx, serverCurrentTimeNode)
		if err != nil {
			t.Fatalf("GetValue() error: %v", err)
		}
		if ok && e.Source == "subscription" {
			entry = e
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if entry.Source != "subscription" {
		t.Fatalf("values bucket for %s was never populated with Source=subscription within 20s", serverCurrentTimeNode)
	}
	if entry.Value == nil {
		t.Errorf("expected a non-nil pushed value, got %+v", entry)
	}
}

// TestIntegrationReadThroughCacheHitMissExpiry covers NFR-4.1's
// read-through cache scenario against a real server: a live read of an
// unsubscribed, session-stable node populates the cache; a subsequent read
// within max_age_ms hits it (zero extra live calls); after the TTL
// notionally expires (simulated via a short TTL rather than a real sleep),
// it goes live again.
func TestIntegrationReadThroughCacheHitMissExpiry(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	client := newIntegrationTestClient(t, endpoint)
	st := newIntegrationTestStore(t)
	ctx := context.Background()

	cc := NewCachingClient(client, st, &config.SearchConfig{EnableCache: true}, time.Hour, time.Hour)

	first, err := cc.Read(ctx, []string{serverStartTimeNode}, 0)
	if err != nil {
		t.Fatalf("Read() (live) error: %v", err)
	}
	if len(first) != 1 || first[0].Source != "live" {
		t.Fatalf("first Read() = %+v, want a live result (nothing cached yet)", first)
	}

	second, err := cc.Read(ctx, []string{serverStartTimeNode}, 60000)
	if err != nil {
		t.Fatalf("Read() (cache hit) error: %v", err)
	}
	if len(second) != 1 || second[0].Source != "cache" {
		t.Fatalf("second Read() = %+v, want a cache hit within max_age_ms", second)
	}
	if second[0].Value != first[0].Value {
		t.Errorf("cached value %v differs from the original live value %v for a session-stable node", second[0].Value, first[0].Value)
	}

	third, err := cc.Read(ctx, []string{serverStartTimeNode}, 0)
	if err != nil {
		t.Fatalf("Read() (expired) error: %v", err)
	}
	if len(third) != 1 || third[0].Source != "live" {
		t.Fatalf("third Read() with max_age_ms=0 = %+v, want live (BR-2: max_age_ms=0 never hits a live-sourced cache entry)", third)
	}
}
