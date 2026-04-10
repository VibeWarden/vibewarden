package log_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func makeEvent(eventType string) events.Event {
	return events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		AISummary:     "test event: " + eventType,
	}
}

func logN(t *testing.T, rb *logadapter.RingBuffer, n int, eventType string) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := rb.Log(context.Background(), makeEvent(eventType)); err != nil {
			t.Fatalf("Log(%d): %v", i, err)
		}
	}
}

// TestRingBuffer_BasicQuery verifies that stored events are returned oldest-first
// and the cursor advances correctly.
func TestRingBuffer_BasicQuery(t *testing.T) {
	rb := logadapter.NewRingBuffer(10)

	logN(t, rb, 3, events.EventTypeAuthSuccess)

	got, cursor := rb.Query(0, nil, 50)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if cursor != got[len(got)-1].Cursor {
		t.Errorf("returned cursor %d != last event cursor %d", cursor, got[len(got)-1].Cursor)
	}
	// Verify ascending order.
	for i := 1; i < len(got); i++ {
		if got[i].Cursor <= got[i-1].Cursor {
			t.Errorf("events not sorted ascending at index %d: cursor %d <= %d", i, got[i].Cursor, got[i-1].Cursor)
		}
	}
}

// TestRingBuffer_SinceCursor verifies that only events after the given cursor
// are returned.
func TestRingBuffer_SinceCursor(t *testing.T) {
	rb := logadapter.NewRingBuffer(10)

	logN(t, rb, 5, events.EventTypeAuthSuccess)

	first, cursor1 := rb.Query(0, nil, 2)
	if len(first) != 2 {
		t.Fatalf("first page: len = %d, want 2", len(first))
	}

	second, _ := rb.Query(cursor1, nil, 50)
	if len(second) != 3 {
		t.Fatalf("second page: len = %d, want 3", len(second))
	}
	// All cursors in second must be > cursor1.
	for _, se := range second {
		if se.Cursor <= cursor1 {
			t.Errorf("second page cursor %d <= since %d", se.Cursor, cursor1)
		}
	}
}

// TestRingBuffer_TypeFilter verifies that the type filter returns only matching
// events.
func TestRingBuffer_TypeFilter(t *testing.T) {
	rb := logadapter.NewRingBuffer(20)

	for i := 0; i < 5; i++ {
		if err := rb.Log(context.Background(), makeEvent(events.EventTypeAuthFailed)); err != nil {
			t.Fatalf("Log auth.failed: %v", err)
		}
		if err := rb.Log(context.Background(), makeEvent(events.EventTypeRateLimitHit)); err != nil {
			t.Fatalf("Log rate_limit.hit: %v", err)
		}
	}

	// Filter for auth.failed only.
	got, _ := rb.Query(0, []string{events.EventTypeAuthFailed}, 50)
	if len(got) != 5 {
		t.Fatalf("filtered len = %d, want 5", len(got))
	}
	for _, se := range got {
		if se.Event.EventType != events.EventTypeAuthFailed {
			t.Errorf("unexpected event type %q", se.Event.EventType)
		}
	}
}

// TestRingBuffer_MultiTypeFilter verifies that multiple event types can be
// requested in a single query.
func TestRingBuffer_MultiTypeFilter(t *testing.T) {
	rb := logadapter.NewRingBuffer(20)

	logN(t, rb, 3, events.EventTypeAuthFailed)
	logN(t, rb, 3, events.EventTypeRateLimitHit)
	logN(t, rb, 3, events.EventTypeProxyStarted)

	got, _ := rb.Query(0, []string{events.EventTypeAuthFailed, events.EventTypeRateLimitHit}, 50)
	if len(got) != 6 {
		t.Fatalf("multi-filter len = %d, want 6", len(got))
	}
	for _, se := range got {
		if se.Event.EventType != events.EventTypeAuthFailed && se.Event.EventType != events.EventTypeRateLimitHit {
			t.Errorf("unexpected event type %q in multi-type filter result", se.Event.EventType)
		}
	}
}

// TestRingBuffer_CapacityOverflow verifies that once the buffer is full, older
// events are overwritten and only the most recent capacity events are visible.
func TestRingBuffer_CapacityOverflow(t *testing.T) {
	const cap = 5
	rb := logadapter.NewRingBuffer(cap)

	// Store 8 events; only the last 5 should be visible.
	for i := 0; i < 8; i++ {
		if err := rb.Log(context.Background(), makeEvent(events.EventTypeAuthSuccess)); err != nil {
			t.Fatalf("Log(%d): %v", i, err)
		}
	}

	got, _ := rb.Query(0, nil, 100)
	if len(got) != cap {
		t.Fatalf("after overflow: len = %d, want %d", len(got), cap)
	}

	// Verify ascending cursor order is preserved after overflow.
	for i := 1; i < len(got); i++ {
		if got[i].Cursor <= got[i-1].Cursor {
			t.Errorf("cursor not ascending at index %d: %d <= %d", i, got[i].Cursor, got[i-1].Cursor)
		}
	}

	// All 8 cursors were allocated; the visible 5 should be the highest ones.
	// After writing 8 events to a cap-5 buffer: cursors 4,5,6,7,8 remain.
	lowest := got[0].Cursor
	if lowest < 4 {
		t.Errorf("lowest visible cursor %d too low — old events still visible", lowest)
	}
}

// TestRingBuffer_LimitParam verifies that the limit parameter caps the result.
func TestRingBuffer_LimitParam(t *testing.T) {
	rb := logadapter.NewRingBuffer(100)

	logN(t, rb, 20, events.EventTypeAuthSuccess)

	got, _ := rb.Query(0, nil, 5)
	if len(got) != 5 {
		t.Fatalf("limit=5: len = %d, want 5", len(got))
	}
}

// TestRingBuffer_EmptyQuery verifies the behaviour when no events match.
func TestRingBuffer_EmptyQuery(t *testing.T) {
	rb := logadapter.NewRingBuffer(10)

	got, cursor := rb.Query(0, nil, 50)
	if len(got) != 0 {
		t.Fatalf("empty buffer: len = %d, want 0", len(got))
	}
	if cursor != 0 {
		t.Errorf("empty buffer: cursor = %d, want 0", cursor)
	}
}

// TestRingBuffer_SinceReturnsNoNewEvents verifies that calling Query with the
// most recent cursor returns no events and preserves the cursor value.
func TestRingBuffer_SinceReturnsNoNewEvents(t *testing.T) {
	rb := logadapter.NewRingBuffer(10)

	logN(t, rb, 3, events.EventTypeAuthSuccess)
	_, cursor := rb.Query(0, nil, 50)

	got, newCursor := rb.Query(cursor, nil, 50)
	if len(got) != 0 {
		t.Fatalf("no-new-events: len = %d, want 0", len(got))
	}
	if newCursor != cursor {
		t.Errorf("no-new-events: cursor changed from %d to %d", cursor, newCursor)
	}
}

// TestRingBuffer_DefaultCapacity verifies that a zero capacity produces a
// buffer with DefaultRingBufferCapacity.
func TestRingBuffer_DefaultCapacity(t *testing.T) {
	rb := logadapter.NewRingBuffer(0)
	logN(t, rb, logadapter.DefaultRingBufferCapacity+10, events.EventTypeAuthSuccess)

	got, _ := rb.Query(0, nil, 10000)
	if len(got) != logadapter.DefaultRingBufferCapacity {
		t.Fatalf("default capacity: len = %d, want %d", len(got), logadapter.DefaultRingBufferCapacity)
	}
}

// TestRingBuffer_ConcurrentWrites verifies that concurrent Log calls do not
// cause data races. This test is most useful when run with -race.
func TestRingBuffer_ConcurrentWrites(t *testing.T) {
	rb := logadapter.NewRingBuffer(100)
	const goroutines = 20
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				ev := makeEvent(fmt.Sprintf("test.event.%d", id))
				if err := rb.Log(context.Background(), ev); err != nil {
					t.Errorf("goroutine %d Log(%d): %v", id, i, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Buffer capacity is 100; total writes is 1000 — only most recent 100 visible.
	got, _ := rb.Query(0, nil, 10000)
	if len(got) != 100 {
		t.Fatalf("after concurrent writes: len = %d, want 100", len(got))
	}
}

// TestRingBuffer_ConcurrentReadWrite verifies that concurrent reads and writes
// do not race or deadlock.
func TestRingBuffer_ConcurrentReadWrite(t *testing.T) {
	rb := logadapter.NewRingBuffer(50)

	var wg sync.WaitGroup
	const writers = 10
	const readers = 5

	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = rb.Log(context.Background(), makeEvent(events.EventTypeAuthSuccess))
			}
		}()
	}

	wg.Add(readers)
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			var cursor uint64
			for i := 0; i < 20; i++ {
				_, cursor = rb.Query(cursor, nil, 10)
			}
		}()
	}

	wg.Wait()
}
