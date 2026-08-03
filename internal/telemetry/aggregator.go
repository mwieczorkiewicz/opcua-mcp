package telemetry

import (
	"sync"
	"sync/atomic"
)

// Error kind constants - the closed set of categories RecordError will ever
// emit in a session_summary payload. Any kind passed to RecordError that
// isn't one of these is recorded as ErrKindOther instead: this is what makes
// "never raw error message text" an enforced invariant rather than a
// convention a call site could accidentally violate (e.g. by passing
// err.Error() instead of a category).
const (
	ErrKindTimeout           = "timeout"
	ErrKindConnectionRefused = "connection_refused"
	ErrKindTypeMismatch      = "type_mismatch"
	ErrKindInvalidArgument   = "invalid_argument"
	ErrKindNotFound          = "not_found"
	ErrKindPermissionDenied  = "permission_denied"
	ErrKindUnavailable       = "unavailable"
	ErrKindOther             = "other"
)

var knownErrorKinds = map[string]bool{
	ErrKindTimeout:           true,
	ErrKindConnectionRefused: true,
	ErrKindTypeMismatch:      true,
	ErrKindInvalidArgument:   true,
	ErrKindNotFound:          true,
	ErrKindPermissionDenied:  true,
	ErrKindUnavailable:       true,
	ErrKindOther:             true,
}

// normalizeErrorKind clamps an arbitrary string to the known error-kind set,
// so nothing but one of the constants above can ever reach a snapshot.
func normalizeErrorKind(kind string) string {
	if knownErrorKinds[kind] {
		return kind
	}
	return ErrKindOther
}

// aggregator holds in-memory usage counters for a single flush interval,
// reset after each flush.
//
// Tool-call and error counts use sync.Map of *atomic.Int64 rather than a
// single mutex-guarded map: an MCP tool call happens on every request, so
// incrementing its counter must never contend a shared lock in that hot
// path. sync.Map's read-mostly design means every call after the first for a
// given key (tool name, or the small closed set of error kinds) only pays
// for a lock-free atomic increment. Cache hits/misses, total call count and
// total param count are plain atomics for the same reason - one int64
// counter each, no map indirection needed. The one place this trades away
// simplicity is snapshot(): reading every counter and resetting it isn't a
// single atomic operation across the whole aggregator, so a session_summary
// payload is a best-effort aggregate, not an exact ledger - acceptable here
// since nothing downstream depends on exact counts.
type aggregator struct {
	toolCalls   sync.Map // string (tool name) -> *atomic.Int64
	errorCounts sync.Map // string (error kind) -> *atomic.Int64

	cacheHits   atomic.Int64
	cacheMisses atomic.Int64

	toolCallCount atomic.Int64
	totalParams   atomic.Int64

	// nodeCount is a gauge (last-write-wins via SetDiscoveryStats), not a
	// per-interval accumulation, so it isn't reset on snapshot.
	nodeCount atomic.Int64
}

func newAggregator() *aggregator {
	return &aggregator{}
}

func (a *aggregator) recordToolCall(name string, paramCount int) {
	a.toolCallCount.Add(1)
	a.totalParams.Add(int64(paramCount))
	counter(&a.toolCalls, name).Add(1)
}

func (a *aggregator) recordCacheResult(hit bool) {
	if hit {
		a.cacheHits.Add(1)
	} else {
		a.cacheMisses.Add(1)
	}
}

func (a *aggregator) recordError(kind string) {
	counter(&a.errorCounts, normalizeErrorKind(kind)).Add(1)
}

func (a *aggregator) setDiscoveryStats(nodeCount int) {
	a.nodeCount.Store(int64(nodeCount))
}

func counter(m *sync.Map, key string) *atomic.Int64 {
	v, _ := m.LoadOrStore(key, &atomic.Int64{})
	return v.(*atomic.Int64)
}

// aggregatorSnapshot is a point-in-time copy of the aggregator's counters,
// with the per-interval ones already reset to zero as part of being read.
type aggregatorSnapshot struct {
	ToolCalls     map[string]int64
	ToolCallCount int64
	TotalParams   int64
	CacheHits     int64
	CacheMisses   int64
	ErrorCounts   map[string]int64
	NodeCount     int64
}

// snapshot copies out current counts and resets every per-interval counter
// for the next flush window.
func (a *aggregator) snapshot() aggregatorSnapshot {
	snap := aggregatorSnapshot{
		ToolCalls:     map[string]int64{},
		ToolCallCount: a.toolCallCount.Swap(0),
		TotalParams:   a.totalParams.Swap(0),
		CacheHits:     a.cacheHits.Swap(0),
		CacheMisses:   a.cacheMisses.Swap(0),
		ErrorCounts:   map[string]int64{},
		NodeCount:     a.nodeCount.Load(),
	}
	a.toolCalls.Range(func(k, v interface{}) bool {
		if n := v.(*atomic.Int64).Swap(0); n > 0 {
			snap.ToolCalls[k.(string)] = n
		}
		return true
	})
	a.errorCounts.Range(func(k, v interface{}) bool {
		if n := v.(*atomic.Int64).Swap(0); n > 0 {
			snap.ErrorCounts[k.(string)] = n
		}
		return true
	})
	return snap
}

// bucketNodeCount maps an exact discovered-node count to a coarse range, so
// the exact size of a deployment's address space is never transmitted.
func bucketNodeCount(n int64) string {
	switch {
	case n < 100:
		return "<100"
	case n < 1000:
		return "100-1k"
	case n < 10000:
		return "1k-10k"
	default:
		return ">10k"
	}
}
