package opcua

import (
	"context"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/store"
	"pgregory.net/rapid"
)

// TestCachingClientReadTTLInvariant is the NFR-3.2 property test: for any
// "live"-sourced ValueEntry, Read must return Source: "cache" if and only
// if the entry is within maxAgeMs of now - the cached <=> within-TTL
// invariant.
//
// ageMs/maxAgeMs are drawn with a fixed safety margin (marginMs) around the
// boundary rather than uniformly at random: real wall-clock time elapses
// between computing ReceivedAt and CachingClient.Read's internal
// time.Since(...) comparison, so an ageMs drawn arbitrarily close to
// maxAgeMs would make the test's own expectation racy against that
// overhead, not a genuine property violation. marginMs is generous
// relative to this in-memory test's actual overhead (sub-millisecond).
func TestCachingClientReadTTLInvariant(t *testing.T) {
	const marginMs = 300

	rapid.Check(t, func(rt *rapid.T) {
		maxAgeMs := rapid.IntRange(marginMs, 60000).Draw(rt, "maxAgeMs")
		withinTTL := rapid.Bool().Draw(rt, "withinTTL")

		var ageMs int
		if withinTTL {
			ageMs = rapid.IntRange(0, maxAgeMs-marginMs).Draw(rt, "ageMs")
		} else {
			ageMs = rapid.IntRange(maxAgeMs+marginMs, maxAgeMs+marginMs+120000).Draw(rt, "ageMs")
		}

		client := newTestClient(t)
		liveCalled := false
		client.client = &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
			liveCalled = true
			return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusOK, Value: ua.MustVariant(int32(1))}}}, nil
		}}
		client.SetConnectedForTesting(true)

		cache := newTestCacheStore(t)
		ctx := context.Background()
		receivedAt := time.Now().Add(-time.Duration(ageMs) * time.Millisecond)
		if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "live", ReceivedAt: receivedAt}); err != nil {
			rt.Fatalf("PutValue() error: %v", err)
		}

		cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
		results, err := cc.Read(ctx, []string{"i=1"}, maxAgeMs)
		if err != nil {
			rt.Fatalf("Read() error: %v", err)
		}
		if len(results) != 1 {
			rt.Fatalf("Read() returned %d results, want 1", len(results))
		}

		gotCacheHit := results[0].Source == "cache"
		if gotCacheHit != withinTTL {
			rt.Fatalf("ageMs=%d maxAgeMs=%d: Source=%s (cache hit=%v), want cache hit=%v", ageMs, maxAgeMs, results[0].Source, gotCacheHit, withinTTL)
		}
		if gotCacheHit && liveCalled {
			rt.Fatalf("ageMs=%d maxAgeMs=%d: cache hit but live Read was also called", ageMs, maxAgeMs)
		}
		if !gotCacheHit && !liveCalled {
			rt.Fatalf("ageMs=%d maxAgeMs=%d: cache miss but live Read was never called", ageMs, maxAgeMs)
		}
	})
}

// TestCachingClientReadZeroMaxAgeNeverHitsCache is a dedicated example
// test for the max_age_ms=0 edge case the property test above
// deliberately excludes (real elapsed time is always > 0, so a
// "live"-sourced entry can never satisfy time.Since(ReceivedAt) <= 0) -
// confirming business-rules.md BR-2's documented behavior explicitly.
func TestCachingClientReadZeroMaxAgeNeverHitsCache(t *testing.T) {
	client := newTestClient(t)
	client.client = &mockOpcuaClient{readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
		return &ua.ReadResponse{Results: []*ua.DataValue{{Status: ua.StatusOK, Value: ua.MustVariant(int32(1))}}}, nil
	}}
	client.SetConnectedForTesting(true)

	cache := newTestCacheStore(t)
	ctx := context.Background()
	if err := cache.PutValue(ctx, "i=1", store.ValueEntry{Value: int32(1), Source: "live", ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	cc := NewCachingClient(client, cache, newEnabledSearchConfig(), time.Hour, time.Hour)
	results, err := cc.Read(ctx, []string{"i=1"}, 0)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(results) != 1 || results[0].Source != "live" {
		t.Fatalf("Read() = %+v, want live (max_age_ms=0 never hits a live-sourced cache entry)", results)
	}
}
