package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBrokerPublishSubscribe(t *testing.T) {
	b := NewEventBroker()
	sub := b.Subscribe(0)
	defer sub.Unsubscribe()

	b.Publish("sample.inserted", "/dev/sda")

	select {
	case ev := <-sub.Events:
		if ev.ID <= 0 {
			t.Fatalf("expected positive event id, got %d", ev.ID)
		}
		if ev.Type != "sample.inserted" {
			t.Fatalf("unexpected event type: %q", ev.Type)
		}
		if ev.Device != "/dev/sda" {
			t.Fatalf("unexpected device: %q", ev.Device)
		}
		if ev.Timestamp.IsZero() {
			t.Fatal("expected timestamp to be set")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestEventBrokerCloseAndSubscribeAfterClose(t *testing.T) {
	b := NewEventBroker()
	sub := b.Subscribe(0)
	b.Close()

	_, ok := <-sub.Events
	if ok {
		t.Fatal("expected existing subscription channel to be closed")
	}

	sub2 := b.Subscribe(0)
	_, ok = <-sub2.Events
	if ok {
		t.Fatal("expected subscribe after close to return closed channel")
	}
}

func TestEventBrokerReplayBufferedEvents(t *testing.T) {
	b := NewEventBroker()
	b.Publish("sample.inserted", "/dev/sda")
	b.Publish("sample.updated", "/dev/sdb")
	b.Publish("sample.deleted", "/dev/sdc")

	seed := b.history[b.historyStart].ID
	sub := b.Subscribe(seed)
	defer sub.Unsubscribe()

	if sub.NeedsResync {
		t.Fatal("expected buffered replay instead of resync")
	}
	if len(sub.Replay) != 2 {
		t.Fatalf("expected 2 replay events, got %d", len(sub.Replay))
	}
	if sub.Replay[0].ID <= seed || sub.Replay[1].ID <= sub.Replay[0].ID {
		t.Fatalf("unexpected replay ids: %+v", []int64{sub.Replay[0].ID, sub.Replay[1].ID})
	}
}

func TestEventBrokerReplayRequiresResyncWhenCursorFallsBehind(t *testing.T) {
	b := NewEventBroker()
	for i := 0; i < eventBufferSize+2; i++ {
		b.Publish("sample.updated", "/dev/sda")
	}

	sub := b.Subscribe(1)
	defer sub.Unsubscribe()

	if !sub.NeedsResync {
		t.Fatal("expected resync when cursor predates buffer")
	}
	if len(sub.Replay) != 0 {
		t.Fatalf("expected no replay when resync required, got %d", len(sub.Replay))
	}
}

func TestEventBrokerReplayRequiresResyncWhenCursorIsAhead(t *testing.T) {
	b := NewEventBroker()
	b.Publish("sample.updated", "/dev/sda")

	sub := b.Subscribe(b.nextID + 1)
	defer sub.Unsubscribe()

	if !sub.NeedsResync {
		t.Fatal("expected resync when cursor is newer than broker state")
	}
}

func TestEventBrokerReplayRequiresResyncWhenHistoryIsEmpty(t *testing.T) {
	b := NewEventBroker()

	sub := b.Subscribe(123)
	defer sub.Unsubscribe()

	if !sub.NeedsResync {
		t.Fatal("expected resync when cursor is present but broker history is empty")
	}
	if len(sub.Replay) != 0 {
		t.Fatalf("expected no replay when history is empty, got %d events", len(sub.Replay))
	}
}

func TestEventBrokerDropsSlowSubscriberInsteadOfSilentlyLosingEvents(t *testing.T) {
	b := NewEventBrokerWithSubscriberBuffer(1)
	sub := b.Subscribe(0)

	b.Publish("sample.first", "/dev/sda")
	b.Publish("sample.overflow", "/dev/sdb")

	ev, ok := <-sub.Events
	if !ok {
		t.Fatal("expected buffered event before channel close")
	}
	if ev.Type != "sample.first" {
		t.Fatalf("expected first event to be preserved, got %q", ev.Type)
	}
	if _, ok := <-sub.Events; ok {
		t.Fatal("expected slow subscriber channel to be closed after overflow")
	}

	stats := b.Stats()
	if stats.DroppedSubscribers != 1 {
		t.Fatalf("expected 1 dropped subscriber, got %+v", stats)
	}
	if stats.DisconnectedSubscribers != 1 {
		t.Fatalf("expected 1 disconnected subscriber, got %+v", stats)
	}
	if !sub.IsDropped() {
		t.Fatal("expected subscription to report dropped state")
	}
}

func TestWriteSSEEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := Event{ID: 42, Type: "test.updated", Device: "/dev/sda", Timestamp: time.Unix(0, 0).UTC()}

	if err := writeSSEEvent(rec, rec, ev); err != nil {
		t.Fatalf("writeSSEEvent failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "id: 42\n") {
		t.Fatalf("missing SSE id line: %q", body)
	}
	if !strings.Contains(body, "event: test.updated\n") {
		t.Fatalf("missing SSE event line: %q", body)
	}
	if !strings.Contains(body, `"id":42`) {
		t.Fatalf("missing JSON payload id: %q", body)
	}
	if !strings.Contains(body, `"type":"test.updated"`) {
		t.Fatalf("missing JSON payload type: %q", body)
	}
	if !strings.Contains(body, `"device":"/dev/sda"`) {
		t.Fatalf("missing JSON payload device: %q", body)
	}
}
