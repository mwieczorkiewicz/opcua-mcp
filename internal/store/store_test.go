package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path, 5*time.Second)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesAllBuckets(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// If a bucket were missing, any of these would panic (nil bucket) or
	// error - exercising all 4 confirms Open's CreateBucketIfNotExists call.
	if _, _, err := s.GetValue(ctx, "i=1"); err != nil {
		t.Errorf("values bucket: %v", err)
	}
	if _, _, err := s.GetTypeInfo(ctx, "i=1"); err != nil {
		t.Errorf("typeinfo bucket: %v", err)
	}
	if _, _, err := s.GetBrowse(ctx, "i=1"); err != nil {
		t.Errorf("browse bucket: %v", err)
	}
	if _, err := s.ListSubscriptions(ctx); err != nil {
		t.Errorf("subscriptions bucket: %v", err)
	}
}

func TestOpenSecondHandleRespectsTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.db")
	first, err := Open(path, 5*time.Second)
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	defer first.Close()

	start := time.Now()
	_, err = Open(path, 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("second Open() against an already-locked file expected an error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("second Open() took %v, want it to respect the ~200ms timeout (not hang)", elapsed)
	}
}

func TestValueRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	want := ValueEntry{
		Value:           int32(42),
		Status:          "StatusOK",
		SourceTimestamp: time.Now().UTC().Truncate(time.Millisecond),
		ServerTimestamp: time.Now().UTC().Truncate(time.Millisecond),
		ReceivedAt:      time.Now().UTC().Truncate(time.Millisecond),
		Source:          "live",
	}

	if err := s.PutValue(ctx, "i=1", want); err != nil {
		t.Fatalf("PutValue() error: %v", err)
	}

	got, ok, err := s.GetValue(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetValue() error: %v", err)
	}
	if !ok {
		t.Fatal("GetValue() ok = false, want true")
	}
	if v, ok := got.Value.(int32); !ok || v != 42 {
		t.Errorf("GetValue().Value = %#v (%T), want int32(42)", got.Value, got.Value)
	}
	if got.Status != want.Status || !got.SourceTimestamp.Equal(want.SourceTimestamp) || got.Source != want.Source {
		t.Errorf("GetValue() = %+v, want %+v", got, want)
	}

	if err := s.DeleteValue(ctx, "i=1"); err != nil {
		t.Fatalf("DeleteValue() error: %v", err)
	}
	_, ok, err = s.GetValue(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetValue() after delete error: %v", err)
	}
	if ok {
		t.Error("GetValue() after DeleteValue() ok = true, want false")
	}
}

func TestGetValueMissingKeyIsNotAnError(t *testing.T) {
	s := openTestStore(t)
	entry, ok, err := s.GetValue(context.Background(), "i=does-not-exist")
	if err != nil {
		t.Fatalf("GetValue() for a missing key returned an error: %v", err)
	}
	if ok {
		t.Error("GetValue() for a missing key returned ok = true")
	}
	if entry.Value != nil {
		t.Errorf("GetValue() for a missing key returned non-zero entry: %+v", entry)
	}
}

func TestEmptyKeyIsRejected(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, _, err := s.GetValue(ctx, ""); err == nil {
		t.Error("GetValue(\"\") expected an error, got nil")
	}
	if err := s.PutValue(ctx, "", ValueEntry{Value: "x"}); err == nil {
		t.Error("PutValue(\"\", ...) expected an error, got nil")
	}
	if err := s.DeleteValue(ctx, ""); err == nil {
		t.Error("DeleteValue(\"\") expected an error, got nil")
	}
}

func TestTypeInfoRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	want := TypeInfoEntry{
		DataTypeID:      6, // Int32
		ValueRank:       -1,
		ArrayDimensions: nil,
		AccessLevel:     0x02,
		UserAccessLevel: 0x02,
		CachedAt:        time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.PutTypeInfo(ctx, "i=1", want); err != nil {
		t.Fatalf("PutTypeInfo() error: %v", err)
	}

	got, ok, err := s.GetTypeInfo(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetTypeInfo() error: %v", err)
	}
	if !ok {
		t.Fatal("GetTypeInfo() ok = false, want true")
	}
	if got.DataTypeID != want.DataTypeID || got.ValueRank != want.ValueRank || got.AccessLevel != want.AccessLevel {
		t.Errorf("GetTypeInfo() = %+v, want %+v", got, want)
	}
}

func TestBrowseRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	want := BrowseEntry{
		References: []BrowseReference{
			{NodeID: "i=2", BrowseName: "Child", DisplayName: "Child", NodeClass: "NodeClassObject", TypeDefinition: "i=58"},
		},
		CachedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.PutBrowse(ctx, "i=1", want); err != nil {
		t.Fatalf("PutBrowse() error: %v", err)
	}

	got, ok, err := s.GetBrowse(ctx, "i=1")
	if err != nil {
		t.Fatalf("GetBrowse() error: %v", err)
	}
	if !ok {
		t.Fatal("GetBrowse() ok = false, want true")
	}
	if len(got.References) != 1 || got.References[0].NodeID != "i=2" {
		t.Errorf("GetBrowse() = %+v, want %+v", got, want)
	}
}

func TestSubscriptionCRUDAndList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	intent1 := SubscriptionIntent{ID: "sub-1", NodeIDs: []string{"i=1", "i=2"}, IntervalMs: 1000, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	intent2 := SubscriptionIntent{ID: "sub-2", NodeIDs: []string{"i=3"}, IntervalMs: 500, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}

	if err := s.PutSubscription(ctx, intent1.ID, intent1); err != nil {
		t.Fatalf("PutSubscription(sub-1) error: %v", err)
	}
	if err := s.PutSubscription(ctx, intent2.ID, intent2); err != nil {
		t.Fatalf("PutSubscription(sub-2) error: %v", err)
	}

	got, ok, err := s.GetSubscription(ctx, "sub-1")
	if err != nil || !ok {
		t.Fatalf("GetSubscription(sub-1) = %+v, %v, %v", got, ok, err)
	}
	if len(got.NodeIDs) != 2 {
		t.Errorf("GetSubscription(sub-1).NodeIDs = %v, want 2 entries", got.NodeIDs)
	}

	all, err := s.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions() error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListSubscriptions() returned %d entries, want 2", len(all))
	}

	if err := s.DeleteSubscription(ctx, "sub-1"); err != nil {
		t.Fatalf("DeleteSubscription(sub-1) error: %v", err)
	}
	all, err = s.ListSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListSubscriptions() after delete error: %v", err)
	}
	if len(all) != 1 || all[0].ID != "sub-2" {
		t.Errorf("ListSubscriptions() after delete = %+v, want only sub-2", all)
	}
}

func TestContextCancellationIsRespected(t *testing.T) {
	s := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := s.GetValue(ctx, "i=1"); err == nil {
		t.Error("GetValue() with a canceled context expected an error, got nil")
	}
	if err := s.PutValue(ctx, "i=1", ValueEntry{Value: "x"}); err == nil {
		t.Error("PutValue() with a canceled context expected an error, got nil")
	}
}
