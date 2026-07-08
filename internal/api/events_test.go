package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBrokerPublishSubscribe(t *testing.T) {
	b := NewEventBroker()
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	b.Publish("sample.inserted", "/dev/sda")

	select {
	case ev := <-ch:
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
	ch, _ := b.Subscribe()
	b.Close()

	_, ok := <-ch
	if ok {
		t.Fatal("expected existing subscription channel to be closed")
	}

	ch2, _ := b.Subscribe()
	_, ok = <-ch2
	if ok {
		t.Fatal("expected subscribe after close to return closed channel")
	}
}

func TestWriteSSEEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := Event{Type: "test.updated", Device: "/dev/sda", Timestamp: time.Unix(0, 0).UTC()}

	if err := writeSSEEvent(rec, rec, ev); err != nil {
		t.Fatalf("writeSSEEvent failed: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: test.updated\n") {
		t.Fatalf("missing SSE event line: %q", body)
	}
	if !strings.Contains(body, `"type":"test.updated"`) {
		t.Fatalf("missing JSON payload type: %q", body)
	}
	if !strings.Contains(body, `"device":"/dev/sda"`) {
		t.Fatalf("missing JSON payload device: %q", body)
	}
}
