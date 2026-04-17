package tracking

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*SessionStore, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewSessionStore(rdb)
	return store, func() {
		rdb.Close()
		mr.Close()
	}
}

func TestSessionStore_GetOrCreate(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	sess, err := store.GetOrCreate(ctx, "s1", "https://cdn.example.com/content.m3u8")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if sess.SessionID != "s1" {
		t.Errorf("want session s1 got %s", sess.SessionID)
	}

	// 再次获取应返回同一个
	sess2, err := store.GetOrCreate(ctx, "s1", "")
	if err != nil {
		t.Fatalf("GetOrCreate again: %v", err)
	}
	if sess2 != sess {
		t.Error("should return same pointer from local cache")
	}
}

func TestSessionStore_RecordSegmentPlayed_Impression(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	_, _ = store.GetOrCreate(ctx, "s2", "https://example.com/content.m3u8")

	evts, err := store.RecordSegmentPlayed(ctx, "s2", "ad1", 3)
	if err != nil {
		t.Fatalf("RecordSegmentPlayed: %v", err)
	}
	if !containsEvent(evts, EventImpression) {
		t.Error("first segment should trigger impression")
	}
}

func TestSessionStore_RecordSegmentPlayed_Complete(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	_, _ = store.GetOrCreate(ctx, "s3", "")

	totalSegs := 3
	var lastEvts []EventType
	for i := 0; i < totalSegs; i++ {
		evts, _ := store.RecordSegmentPlayed(ctx, "s3", "adX", totalSegs)
		lastEvts = evts
	}
	if !containsEvent(lastEvts, EventComplete) {
		t.Error("last segment should trigger complete")
	}
}

func TestSessionStore_GetEvents(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	_, _ = store.GetOrCreate(ctx, "s4", "")
	_, _ = store.RecordSegmentPlayed(ctx, "s4", "ad2", 2)

	events, err := store.GetEvents(ctx, "s4")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one event")
	}
}

func TestQuartileEvents(t *testing.T) {
	cases := []struct {
		played int
		total  int
		want   EventType
	}{
		{1, 4, EventFirstQuartile},
		{2, 4, EventMidpoint},
		{3, 4, EventThirdQuartile},
		{4, 4, EventComplete},
	}
	for _, c := range cases {
		evts := quartileEvents(c.played, c.total)
		if !containsEvent(evts, c.want) {
			t.Errorf("played=%d total=%d: want %s in %v", c.played, c.total, c.want, evts)
		}
	}
}

func TestRecordSegmentPlayed_ImpressionViaStore(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()
	_, _ = store.GetOrCreate(ctx, "s5", "")
	evts, _ := store.RecordSegmentPlayed(ctx, "s5", "adY", 4)
	if !containsEvent(evts, EventImpression) {
		t.Error("first segment should trigger impression via RecordSegmentPlayed")
	}
}

func TestBeaconProxy_FireSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skip network test")
	}
	bp := NewBeaconProxy(5)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 发送到 localhost（不存在，期望 error 但不 panic）
	errs := bp.FireSync(ctx, []string{"http://127.0.0.1:19999/beacon"})
	if len(errs) != 1 {
		t.Errorf("expected 1 error result, got %d", len(errs))
	}
	// error 可以是 connection refused，不要 panic
}

func containsEvent(evts []EventType, target EventType) bool {
	for _, e := range evts {
		if e == target {
			return true
		}
	}
	return false
}
