package opcua

import (
	"context"
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// modelSubscription mirrors what SubscriptionManager should be tracking for
// one logical subscription, per the simplified reference model (NFR
// Requirements Q2, PBT-06).
type modelSubscription struct {
	nodeIDs    []string
	intervalMs int
}

// TestSubscriptionManagerStatefulProperties drives random Subscribe/
// Unsubscribe command sequences against a real SubscriptionManager (backed
// by the mocked subscribingClient seam, which accepts every node by
// default), asserting after every command that ListSubscriptions matches a
// simplified reference model and that each intervalGroup's RefCount
// invariant (RefCount == number of model subscriptions referencing that
// interval) holds - exactly PBT-06's shape.
func TestSubscriptionManagerStatefulProperties(t *testing.T) {
	intervals := []int{100, 500, 1000} // small, fixed set to force interval sharing (BR-2)

	rapid.Check(t, func(rt *rapid.T) {
		client := &mockSubscribingClient{}
		cache := newTestStoreForSubscription(t)
		mgr := NewSubscriptionManager(client, cache, newTestStoreConfig(t))

		ctx := context.Background()
		if err := mgr.Start(ctx); err != nil {
			rt.Fatalf("Start() error: %v", err)
		}
		defer mgr.Stop()

		model := make(map[string]modelSubscription) // keyed by SubscriptionManager's real returned ID

		checkInvariants := func(rt *rapid.T) {
			list := mgr.ListSubscriptions()
			if len(list) != len(model) {
				rt.Fatalf("ListSubscriptions() has %d entries, model has %d", len(list), len(model))
			}
			for _, info := range list {
				want, ok := model[info.ID]
				if !ok {
					rt.Fatalf("ListSubscriptions() has unexpected ID %s not in model", info.ID)
				}
				if info.IntervalMs != want.intervalMs || len(info.NodeIDs) != len(want.nodeIDs) {
					rt.Fatalf("ListSubscriptions()[%s] = %+v, want interval %d with %d nodes", info.ID, info, want.intervalMs, len(want.nodeIDs))
				}
			}

			expectedRefCount := make(map[int]int)
			for _, sub := range model {
				expectedRefCount[sub.intervalMs]++
			}
			mgr.mu.RLock()
			for intervalMs, group := range mgr.intervalGroups {
				if group.RefCount != expectedRefCount[intervalMs] {
					mgr.mu.RUnlock()
					rt.Fatalf("intervalGroups[%d].RefCount = %d, want %d (model subscriptions at that interval)", intervalMs, group.RefCount, expectedRefCount[intervalMs])
				}
			}
			mgr.mu.RUnlock()
			for intervalMs, count := range expectedRefCount {
				if count > 0 {
					mgr.mu.RLock()
					_, ok := mgr.intervalGroups[intervalMs]
					mgr.mu.RUnlock()
					if !ok {
						rt.Fatalf("model expects an active intervalGroup at %dms, but none exists", intervalMs)
					}
				}
			}
		}

		rt.Repeat(map[string]func(*rapid.T){
			"": checkInvariants,
			"Subscribe": func(rt *rapid.T) {
				n := rapid.IntRange(1, 3).Draw(rt, "nodeCount")
				nodeIDs := make([]string, n)
				for i := 0; i < n; i++ {
					nodeIDs[i] = fmt.Sprintf("i=%d", rapid.IntRange(1, 20).Draw(rt, "nodeID"))
				}
				intervalMs := rapid.SampledFrom(intervals).Draw(rt, "intervalMs")

				id, rejected, err := mgr.Subscribe(ctx, nodeIDs, intervalMs)
				if err != nil {
					// All-rejected is impossible here (the mock accepts
					// everything by default), so any error is unexpected.
					rt.Fatalf("Subscribe() error: %v", err)
				}
				if len(rejected) != 0 {
					rt.Fatalf("Subscribe() rejected = %+v, want none (mock accepts all)", rejected)
				}
				model[id] = modelSubscription{nodeIDs: nodeIDs, intervalMs: intervalMs}
			},
			"Unsubscribe": func(rt *rapid.T) {
				if len(model) == 0 {
					rt.Skip("no active subscriptions to remove")
				}
				ids := make([]string, 0, len(model))
				for id := range model {
					ids = append(ids, id)
				}
				id := rapid.SampledFrom(ids).Draw(rt, "id")

				if err := mgr.Unsubscribe(ctx, id); err != nil {
					rt.Fatalf("Unsubscribe(%s) error: %v", id, err)
				}
				delete(model, id)
			},
		})
	})
}
