package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	eventBufferSize          = 256
	defaultSubscriberBufSize = 16
)

type Event struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Device    string    `json:"device,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type EventBroker struct {
	mu              sync.Mutex
	subs            map[*subscriber]struct{}
	closed          bool
	nextID          int64
	history         []Event
	historyStart    int
	subBufSize      int
	dropCount       int
	disconnectCount int
}

type subscriber struct {
	ch      chan Event
	dropped bool
}

type Subscription struct {
	Events      chan Event
	Replay      []Event
	NeedsResync bool
	IsDropped   func() bool
	Unsubscribe func()
}

func NewEventBroker() *EventBroker {
	return NewEventBrokerWithSubscriberBuffer(defaultSubscriberBufSize)
}

func NewEventBrokerWithSubscriberBuffer(subscriberBufferSize int) *EventBroker {
	baseID := time.Now().UTC().UnixNano()
	if baseID < 1 {
		baseID = 1
	}
	if subscriberBufferSize <= 0 {
		subscriberBufferSize = defaultSubscriberBufSize
	}
	return &EventBroker{
		subs:       make(map[*subscriber]struct{}),
		history:    make([]Event, 0, eventBufferSize),
		nextID:     baseID - 1,
		subBufSize: subscriberBufferSize,
	}
}

func (b *EventBroker) Subscribe(lastEventID int64) Subscription {
	sub := &subscriber{ch: make(chan Event, b.subBufSize)}
	b.mu.Lock()
	if b.closed {
		close(sub.ch)
		b.mu.Unlock()
		return Subscription{Events: sub.ch, IsDropped: func() bool { return false }, Unsubscribe: func() {}}
	}

	replay, needsResync := b.replayLocked(lastEventID)
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	return Subscription{
		Events:      sub.ch,
		Replay:      replay,
		NeedsResync: needsResync,
		IsDropped: func() bool {
			b.mu.Lock()
			dropped := sub.dropped
			b.mu.Unlock()
			return dropped
		},
		Unsubscribe: func() {
			b.mu.Lock()
			b.unsubscribeLocked(sub)
			b.mu.Unlock()
		},
	}
}

func (b *EventBroker) Publish(eventType string, device string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	ev := b.newEventLocked(eventType, device)
	b.appendHistoryLocked(ev)
	for sub := range b.subs {
		select {
		case sub.ch <- ev:
		default:
			sub.dropped = true
			b.dropCount++
			b.disconnectCount++
			b.unsubscribeLocked(sub)
		}
	}
}

type EventBrokerStats struct {
	DroppedSubscribers      int
	DisconnectedSubscribers int
}

func (b *EventBroker) Stats() EventBrokerStats {
	if b == nil {
		return EventBrokerStats{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return EventBrokerStats{
		DroppedSubscribers:      b.dropCount,
		DisconnectedSubscribers: b.disconnectCount,
	}
}

func (b *EventBroker) NewControlEvent(eventType string) Event {
	if b == nil {
		return Event{ID: time.Now().UTC().UnixNano(), Type: eventType, Timestamp: time.Now().UTC()}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.newEventLocked(eventType, "")
}

func (b *EventBroker) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for sub := range b.subs {
		b.unsubscribeLocked(sub)
	}
}

func (b *EventBroker) unsubscribeLocked(sub *subscriber) {
	if _, ok := b.subs[sub]; !ok {
		return
	}
	delete(b.subs, sub)
	close(sub.ch)
}

func (b *EventBroker) newEventLocked(eventType string, device string) Event {
	b.nextID++
	return Event{
		ID:        b.nextID,
		Type:      eventType,
		Device:    device,
		Timestamp: time.Now().UTC(),
	}
}

func (b *EventBroker) appendHistoryLocked(ev Event) {
	if len(b.history) < cap(b.history) {
		b.history = append(b.history, ev)
		return
	}
	b.history[b.historyStart] = ev
	b.historyStart = (b.historyStart + 1) % len(b.history)
}

func (b *EventBroker) replayLocked(lastEventID int64) ([]Event, bool) {
	if lastEventID <= 0 {
		return nil, false
	}
	if len(b.history) == 0 {
		return nil, true
	}

	newestID := b.nextID
	if lastEventID > newestID {
		return nil, true
	}

	oldestID := b.history[b.historyStart].ID
	if lastEventID < oldestID-1 {
		return nil, true
	}

	replay := make([]Event, 0, len(b.history))
	for i := 0; i < len(b.history); i++ {
		ev := b.history[(b.historyStart+i)%len(b.history)]
		if ev.ID > lastEventID {
			replay = append(replay, ev)
		}
	}
	return replay, false
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\n", ev.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", ev.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
