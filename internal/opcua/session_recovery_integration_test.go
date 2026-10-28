//go:build integration

package opcua

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// countDials wraps c's real dialer so a test can tell whether session
// recovery (rather than gopcua's own reconnect) rebuilt the connection.
func countDials(c *Client) *atomic.Int32 {
	var dials atomic.Int32
	c.dial = func(ctx context.Context, gen uint64) (opcuaClient, error) {
		dials.Add(1)
		return c.dialGopcua(ctx, gen)
	}
	return &dials
}

// TestIntegrationRecoversFromServerSideSessionClose has the real server
// delete the client's session (a CloseSession request sent on the raw
// channel, which leaves gopcua holding the now-unknown session ID), so the
// next request is rejected by the server itself with BadSessionIDInvalid.
// Client.Read must reconnect once and return the retried read's result.
func TestIntegrationRecoversFromServerSideSessionClose(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	c := newIntegrationTestClient(t, endpoint)
	dials := countDials(c)
	ctx := context.Background()

	raw, _ := c.snapshot()
	gc, ok := raw.(*opcua.Client)
	if !ok {
		t.Fatalf("underlying client is %T, want *opcua.Client", raw)
	}
	if err := gc.Send(ctx, &ua.CloseSessionRequest{DeleteSubscriptions: true}, func(ua.Response) error { return nil }); err != nil {
		t.Fatalf("server-side CloseSession failed: %v", err)
	}

	// Confirm the server now rejects the old session, so the test exercises
	// a real server-side invalidation rather than a no-op.
	_, err := gc.Read(ctx, &ua.ReadRequest{NodesToRead: []*ua.ReadValueID{{NodeID: ua.MustParseNodeID(serverStartTimeNode), AttributeID: ua.AttributeIDValue}}})
	if !errors.Is(err, ua.StatusBadSessionIDInvalid) {
		t.Fatalf("raw read after CloseSession returned %v, want BadSessionIDInvalid", err)
	}

	results, err := c.Read(ctx, []string{serverStartTimeNode})
	if err != nil {
		t.Fatalf("Read() after server-side session close error: %v", err)
	}
	if len(results) != 1 || results[0].Status != ua.StatusOK {
		t.Fatalf("Read() = %+v, want one Good result", results)
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("session recovery reconnected %d times, want 1", got)
	}

	outcome, err := c.EnsureConnected(ctx)
	if err != nil || outcome != ConnectOutcomeAlreadyHealthy {
		t.Errorf("EnsureConnected() after recovery = %v, %v; want AlreadyHealthy, nil", outcome, err)
	}
}

// TestIntegrationEnsureConnectedRebuildsInvalidSession covers opcua_connect's
// health check against a real server: with the session closed server-side,
// the connected flag is still true, but EnsureConnected must detect the dead
// session and rebuild it.
func TestIntegrationEnsureConnectedRebuildsInvalidSession(t *testing.T) {
	endpoint := startOPCUATestServer(t)
	c := newIntegrationTestClient(t, endpoint)
	ctx := context.Background()

	raw, _ := c.snapshot()
	gc := raw.(*opcua.Client)
	if err := gc.Send(ctx, &ua.CloseSessionRequest{DeleteSubscriptions: true}, func(ua.Response) error { return nil }); err != nil {
		t.Fatalf("server-side CloseSession failed: %v", err)
	}
	if !c.IsConnected() {
		t.Fatal("precondition: client should still believe it is connected")
	}

	outcome, err := c.EnsureConnected(ctx)
	if err != nil {
		t.Fatalf("EnsureConnected() error: %v", err)
	}
	if outcome != ConnectOutcomeReconnected {
		t.Errorf("EnsureConnected() outcome = %v, want Reconnected", outcome)
	}
	if _, err := c.Read(ctx, []string{serverStartTimeNode}); err != nil {
		t.Errorf("Read() after EnsureConnected error: %v", err)
	}
}

// tcpProxy forwards a fixed local port to a retargetable address. Docker
// assigns a new host port when a container restarts, so the proxy gives the
// client one endpoint that survives the restart.
type tcpProxy struct {
	ln     net.Listener
	mu     sync.Mutex
	target string
	conns  []net.Conn
}

func newTCPProxy(t *testing.T, target string) *tcpProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := &tcpProxy{ln: ln, target: target}
	go p.serve()
	t.Cleanup(func() {
		ln.Close()
		p.dropConns()
	})
	return p
}

func (p *tcpProxy) serve() {
	for {
		in, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		target := p.target
		p.mu.Unlock()
		out, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			in.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, in, out)
		p.mu.Unlock()
		go func() { io.Copy(out, in); out.Close() }()
		go func() { io.Copy(in, out); in.Close() }()
	}
}

// retarget points new connections at target and drops existing ones.
func (p *tcpProxy) retarget(target string) {
	p.mu.Lock()
	p.target = target
	p.mu.Unlock()
	p.dropConns()
}

func (p *tcpProxy) dropConns() {
	p.mu.Lock()
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// startRestartableOPCUATestServer starts the test server and returns the
// container plus its current host:port.
func startRestartableOPCUATestServer(t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	ctx := context.Background()

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        opcuaTestServerImage,
			ExposedPorts: []string{"4840/tcp"},
			Cmd:          []string{"--sample", "--port", "4840"},
			WaitingFor:   wait.ForListeningPort("4840/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start %s: %v", opcuaTestServerImage, err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate OPC-UA test server container: %v", err)
		}
	})

	time.Sleep(2 * time.Second)
	return c, containerAddr(t, c)
}

func containerAddr(t *testing.T, c testcontainers.Container) string {
	t.Helper()
	ctx := context.Background()
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "4840")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	return net.JoinHostPort(host, port.Port())
}

// TestIntegrationRecoversAfterServerRestart restarts the server process
// mid-session: every session the client held is gone. Once the server is
// back, reads must work again without a manual opcua_connect, whether
// gopcua's own reconnect or session recovery gets there first, and
// opcua_connect's health check must report a usable connection.
func TestIntegrationRecoversAfterServerRestart(t *testing.T) {
	container, addr := startRestartableOPCUATestServer(t)
	proxy := newTCPProxy(t, addr)
	c := newIntegrationTestClient(t, "opc.tcp://"+proxy.ln.Addr().String())
	ctx := context.Background()

	before, err := c.Read(ctx, []string{serverStartTimeNode})
	if err != nil {
		t.Fatalf("Read() before restart error: %v", err)
	}

	stopTimeout := 5 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("restart container: %v", err)
	}
	proxy.retarget(containerAddr(t, container))
	time.Sleep(3 * time.Second)

	var after []*ua.DataValue
	deadline := time.Now().Add(60 * time.Second)
	for {
		after, err = c.Read(ctx, []string{serverStartTimeNode})
		if err == nil && len(after) == 1 && after[0].Status == ua.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Read() never recovered after server restart, last error: %v", err)
		}
		time.Sleep(time.Second)
	}

	if before[0].Value.Value() == after[0].Value.Value() {
		t.Error("server StartTime unchanged, so the server did not actually restart")
	}
	if _, err := c.EnsureConnected(ctx); err != nil {
		t.Errorf("EnsureConnected() after restart error: %v", err)
	}
	if !c.IsConnected() {
		t.Error("client not connected after recovery")
	}
}
