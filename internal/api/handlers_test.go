package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"diskmon/internal/storage"

	"github.com/go-chi/chi/v5"
)

type fakeHandlerStore struct {
	readyErr      error
	listDrivesErr error
	getDriveItem  *storage.DriveDetail
	getDriveErr   error
	historyErr    error
	attrsErr      error
	testsErr      error
}

func (f fakeHandlerStore) Ready(context.Context) error {
	return f.readyErr
}

func (f fakeHandlerStore) ListDrives(context.Context) ([]storage.DriveSummary, error) {
	return nil, f.listDrivesErr
}

func (f fakeHandlerStore) GetDrive(context.Context, int64) (*storage.DriveDetail, error) {
	return f.getDriveItem, f.getDriveErr
}

func (f fakeHandlerStore) DriveHistory(context.Context, int64, int) ([]storage.HistoryPoint, error) {
	return nil, f.historyErr
}

func (f fakeHandlerStore) DriveAttributes(context.Context, int64) ([]storage.AttributePoint, error) {
	return nil, f.attrsErr
}

func (f fakeHandlerStore) DriveTestRuns(context.Context, int64, int, int) (*storage.SmartTestRunPage, error) {
	return nil, f.testsErr
}

type sentinelError struct{}

func (sentinelError) Error() string {
	return "duckdb open /private/path token=secret failed"
}

func withRouteID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func newBufferedLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{}))
}

// syncRecorder wraps httptest.ResponseRecorder with a mutex so concurrent
// reads of Body from the test goroutine do not race with handler writes.
type syncRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func newSyncRecorder() *syncRecorder {
	return &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *syncRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(p)
}

func (r *syncRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Body.String()
}

type gatedEventStreamRecorder struct {
	*httptest.ResponseRecorder
	mu           sync.Mutex
	blockOnEvent int32
	release      chan struct{}
	eventCount   atomic.Int32
	body         bytes.Buffer
}

func newGatedEventStreamRecorder(blockOnEvent int) *gatedEventStreamRecorder {
	r := &gatedEventStreamRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		blockOnEvent:     int32(blockOnEvent),
		release:          make(chan struct{}),
	}
	// Replace the underlying body with a synchronized buffer so concurrent
	// reads from the test goroutine do not race with handler writes.
	r.ResponseRecorder.Body = &r.body
	return r
}

func (r *gatedEventStreamRecorder) Write(p []byte) (int, error) {
	if bytes.HasPrefix(p, []byte("id: ")) && r.eventCount.Add(1) >= r.blockOnEvent {
		<-r.release
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(p)
}

// BodyString returns a snapshot of the recorded body safe for concurrent use.
func (r *gatedEventStreamRecorder) BodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Body.String()
}

func (r *gatedEventStreamRecorder) Release() {
	select {
	case <-r.release:
	default:
		close(r.release)
	}
}

func TestParsePositiveInt(t *testing.T) {
	if got := parsePositiveInt("", 10); got != 10 {
		t.Fatalf("empty should fallback: got %d", got)
	}
	if got := parsePositiveInt("abc", 10); got != 10 {
		t.Fatalf("invalid should fallback: got %d", got)
	}
	if got := parsePositiveInt("-1", 10); got != 10 {
		t.Fatalf("non-positive should fallback: got %d", got)
	}
	if got := parsePositiveInt("25", 10); got != 25 {
		t.Fatalf("valid should parse: got %d", got)
	}
}

func TestParseIDInvalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/drives/not-a-number", nil)
	req = withRouteID(req, "not-a-number")
	rec := httptest.NewRecorder()

	_, ok := parseID(rec, req)
	if ok {
		t.Fatal("expected parseID to fail for invalid id")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"invalid id"`) {
		t.Fatalf("expected invalid id JSON error, got %q", rec.Body.String())
	}
}

func TestEventsHandlerUnavailable(t *testing.T) {
	h := &Handlers{events: nil}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	h.Events(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "event stream unavailable") {
		t.Fatalf("expected event stream unavailable message, got %q", rec.Body.String())
	}
}

func TestParseLastEventID(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		header  string
		id      int64
		hasID   bool
		isValid bool
	}{
		{name: "missing", target: "/api/v1/events", id: 0, hasID: false, isValid: false},
		{name: "query", target: "/api/v1/events?last_event_id=12", id: 12, hasID: true, isValid: true},
		{name: "header wins", target: "/api/v1/events?last_event_id=12", header: "15", id: 15, hasID: true, isValid: true},
		{name: "invalid", target: "/api/v1/events?last_event_id=abc", id: 0, hasID: true, isValid: false},
		{name: "zero", target: "/api/v1/events?last_event_id=0", id: 0, hasID: true, isValid: false},
		{name: "negative", target: "/api/v1/events", header: "-9", id: 0, hasID: true, isValid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.header != "" {
				req.Header.Set("Last-Event-ID", tt.header)
			}

			id, hasID, isValid := parseLastEventID(req)
			if id != tt.id || hasID != tt.hasID || isValid != tt.isValid {
				t.Fatalf("unexpected parse result: got (%d, %t, %t), want (%d, %t, %t)", id, hasID, isValid, tt.id, tt.hasID, tt.isValid)
			}
		})
	}
}

func TestEventsHandlerResyncsInvalidLastEventID(t *testing.T) {
	h := &Handlers{events: NewEventBroker()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", "abc")
	rec := httptest.NewRecorder()

	h.Events(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"retry: 5000\n\n", "event: stream.resync\n", `"type":"stream.resync"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
	ids := parseSSEIDs(t, body)
	if len(ids) != 1 || ids[0] <= 0 {
		t.Fatalf("expected one positive resync event id, got %v in %q", ids, body)
	}
}

func TestEventsHandlerReplaysBufferedEventsBeforeLiveEvents(t *testing.T) {
	b := NewEventBroker()
	b.Publish("sample.inserted", "/dev/sda")
	b.Publish("sample.updated", "/dev/sdb")
	b.Publish("sample.deleted", "/dev/sdc")
	lastSeenID := b.history[b.historyStart].ID

	h := &Handlers{events: b}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(lastSeenID, 10))
	rec := newSyncRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)
		h.Events(rec, req)
	}()

	waitForSyncBodyContains(t, rec, "event: sample.deleted\n")
	b.Publish("sample.live", "/dev/sdd")
	waitForSyncBodyContains(t, rec, "event: sample.live\n")
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event stream handler to exit")
	}

	body := rec.BodyString()
	positions := map[string]int{
		"sample.updated": strings.Index(body, "event: sample.updated\n"),
		"sample.deleted": strings.Index(body, "event: sample.deleted\n"),
		"sample.live":    strings.Index(body, "event: sample.live\n"),
	}
	for name, pos := range positions {
		if pos < 0 {
			t.Fatalf("expected %s in stream body, got %q", name, body)
		}
	}
	if !(positions["sample.updated"] < positions["sample.deleted"] && positions["sample.deleted"] < positions["sample.live"]) {
		t.Fatalf("expected replay events before live events, got positions %+v in %q", positions, body)
	}
	ids := parseSSEIDs(t, body)
	if len(ids) != 3 {
		t.Fatalf("expected 3 SSE ids, got %v in %q", ids, body)
	}
	if !(ids[0] > lastSeenID && ids[1] > ids[0] && ids[2] > ids[1]) {
		t.Fatalf("expected strictly increasing ids after replay cursor %d, got %v", lastSeenID, ids)
	}
}

func TestEventsHandlerResyncsWhenCursorFallsBehindBuffer(t *testing.T) {
	b := NewEventBroker()
	for i := 0; i < eventBufferSize+2; i++ {
		b.Publish("sample.updated", "/dev/sda")
	}

	h := &Handlers{events: b}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	rec := httptest.NewRecorder()

	h.Events(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: stream.resync\n") {
		t.Fatalf("expected resync event, got %q", body)
	}
	if strings.Contains(body, "event: sample.updated\n") {
		t.Fatalf("expected no buffered replay when resyncing, got %q", body)
	}
}

func TestEventsHandlerResyncsWhenCursorIsInTheFuture(t *testing.T) {
	b := NewEventBroker()
	b.Publish("sample.updated", "/dev/sda")

	h := &Handlers{events: b}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(b.nextID+100, 10))
	rec := httptest.NewRecorder()

	h.Events(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: stream.resync\n") {
		t.Fatalf("expected resync event for future cursor, got %q", body)
	}
	if strings.Contains(body, "event: sample.updated\n") {
		t.Fatalf("expected no replay for future cursor, got %q", body)
	}
}

func TestEventsHandlerResyncsAfterBrokerRestartIDReset(t *testing.T) {
	previous := NewEventBroker()
	previous.Publish("sample.updated", "/dev/sda")
	lastSeenID := previous.nextID

	h := &Handlers{events: NewEventBroker()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(lastSeenID, 10))
	rec := httptest.NewRecorder()

	h.Events(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: stream.resync\n") {
		t.Fatalf("expected resync event after broker restart, got %q", body)
	}
	if strings.Count(body, "id: ") != 1 {
		t.Fatalf("expected exactly one SSE event after restart resync, got %q", body)
	}
}

func TestEventsHandlerClosesDroppedSubscriberWithVisibleResync(t *testing.T) {
	b := NewEventBrokerWithSubscriberBuffer(1)
	h := &Handlers{events: b}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rec := newGatedEventStreamRecorder(2)
	done := make(chan struct{})

	go func() {
		defer close(done)
		h.Events(rec, req)
	}()

	waitForSubscriberCount(t, b, 1)
	b.Publish("sample.first", "/dev/sda")
	waitForGatedBodyContains(t, rec, "event: sample.first\n")
	b.Publish("sample.second", "/dev/sdb")
	b.Publish("sample.third", "/dev/sdc")
	b.Publish("sample.overflow", "/dev/sdd")
	rec.Release()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for dropped event stream handler to exit")
	}

	body := rec.BodyString()
	for _, want := range []string{"event: sample.first\n", "event: sample.second\n", "event: sample.third\n", "event: stream.resync\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected dropped stream body to contain %q, got %q", want, body)
		}
	}
	if strings.Contains(body, "event: sample.overflow\n") {
		t.Fatalf("expected overflow event to force resync instead of silent loss, got %q", body)
	}

	stats := b.Stats()
	if stats.DroppedSubscribers != 1 || stats.DisconnectedSubscribers != 1 {
		t.Fatalf("expected one dropped/disconnected subscriber, got %+v", stats)
	}
}

func TestEventsHandlerExitsWhenBrokerCloses(t *testing.T) {
	b := NewEventBroker()
	h := &Handlers{events: b}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)
		h.Events(rec, req)
	}()

	waitForSubscriberCount(t, b, 1)
	b.Close()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for closed broker stream handler to exit")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "retry: 5000\n\n") {
		t.Fatalf("expected initial retry directive, got %q", body)
	}
	if strings.Contains(body, "event: stream.resync\n") {
		t.Fatalf("expected broker close to end stream without resync, got %q", body)
	}
}

func parseSSEIDs(t *testing.T, body string) []int64 {
	t.Helper()
	var ids []int64
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "id: ") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
		if err != nil {
			t.Fatalf("parse SSE id %q: %v", line, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func waitForGatedBodyContains(t *testing.T, rec *gatedEventStreamRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.BodyString(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, rec.BodyString())
}

func waitForSyncBodyContains(t *testing.T, rec *syncRecorder, want string) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.BodyString(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, rec.BodyString())
}

func waitForSubscriberCount(t *testing.T, b *EventBroker, want int) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		count := len(b.subs)
		b.mu.Unlock()
		if count == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.mu.Lock()
	count := len(b.subs)
	b.mu.Unlock()
	t.Fatalf("timed out waiting for %d subscribers, got %d", want, count)
}

func TestHealthz(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h.Healthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected ok status, got %q", rec.Body.String())
	}
}

func TestReadyzStorageUnavailable(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"storage unavailable"`) {
		t.Fatalf("expected storage unavailable message, got %q", rec.Body.String())
	}
}

func TestReadyzStorageFailure(t *testing.T) {
	h := &Handlers{logger: slog.Default(), db: fakeHandlerStore{readyErr: errors.New("boom")}}
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"not ready"`) {
		t.Fatalf("expected not ready message, got %q", rec.Body.String())
	}
}

func TestListDrivesStorageFailure(t *testing.T) {
	var logs bytes.Buffer
	h := &Handlers{logger: newBufferedLogger(&logs), db: fakeHandlerStore{listDrivesErr: sentinelError{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/drives", nil)
	rec := httptest.NewRecorder()

	h.ListDrives(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"internal server error"`) {
		t.Fatalf("expected sanitized internal error, got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "duckdb open /private/path token=secret failed") {
		t.Fatalf("response leaked raw error: %q", rec.Body.String())
	}
	logOutput := logs.String()
	for _, want := range []string{"internal request failure", `operation="list drives"`, "method=GET", "path=/api/v1/drives", "status=500"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, logOutput)
		}
	}
	if strings.Contains(logOutput, "duckdb open /private/path token=secret failed") {
		t.Fatalf("logs leaked raw error: %q", logOutput)
	}
}

func TestListDrivesStorageFailureNilLogger(t *testing.T) {
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(oldDefault) })

	h := &Handlers{db: fakeHandlerStore{listDrivesErr: sentinelError{}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/drives", nil)
	rec := httptest.NewRecorder()

	h.ListDrives(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"internal server error"`) {
		t.Fatalf("expected sanitized internal error, got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "duckdb open /private/path token=secret failed") {
		t.Fatalf("response leaked raw error: %q", rec.Body.String())
	}
}

func TestGetDriveStorageFailure(t *testing.T) {
	var logs bytes.Buffer
	h := &Handlers{logger: newBufferedLogger(&logs), db: fakeHandlerStore{getDriveErr: sentinelError{}}}
	req := withRouteID(httptest.NewRequest(http.MethodGet, "/api/v1/drives/42", nil), "42")
	rec := httptest.NewRecorder()

	h.GetDrive(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"internal server error"`) {
		t.Fatalf("expected sanitized internal error, got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "duckdb open /private/path token=secret failed") {
		t.Fatalf("response leaked raw error: %q", rec.Body.String())
	}
	logOutput := logs.String()
	for _, want := range []string{`operation="get drive"`, "drive_id=42", "status=500"} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, logOutput)
		}
	}
	if strings.Contains(logOutput, "duckdb open /private/path token=secret failed") {
		t.Fatalf("logs leaked raw error: %q", logOutput)
	}
}

func TestGetDriveResponseIncludesHealthGuidance(t *testing.T) {
	h := &Handlers{db: fakeHandlerStore{getDriveItem: &storage.DriveDetail{
		ID:            42,
		Device:        "/dev/disk42",
		Model:         "Test Model",
		Serial:        "ABC123",
		WWN:           "wwn-42",
		Health:        "RED",
		HealthScore:   10,
		HealthReasons: "PENDING_SECTORS_NONZERO,UDMA_CRC_ERRORS_NONZERO",
	}}}
	req := withRouteID(httptest.NewRequest(http.MethodGet, "/api/v1/drives/42", nil), "42")
	rec := httptest.NewRecorder()

	h.GetDrive(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	for key, want := range map[string]any{
		"id":             float64(42),
		"device":         "/dev/disk42",
		"model":          "Test Model",
		"serial":         "ABC123",
		"wwn":            "wwn-42",
		"health":         "RED",
		"health_score":   float64(10),
		"health_reasons": "PENDING_SECTORS_NONZERO,UDMA_CRC_ERRORS_NONZERO",
	} {
		if got := payload[key]; got != want {
			t.Fatalf("expected %s=%#v, got %#v", key, want, got)
		}
	}

	guidance, ok := payload["health_guidance"].([]any)
	if !ok {
		t.Fatalf("expected health_guidance array, got %#v", payload["health_guidance"])
	}
	if len(guidance) != 2 {
		t.Fatalf("expected 2 guidance entries, got %d", len(guidance))
	}
	for i, entry := range guidance {
		if _, ok := entry.(string); !ok {
			t.Fatalf("expected guidance[%d] to be string, got %#v", i, entry)
		}
	}
}

func TestGetDriveNotFound(t *testing.T) {
	h := &Handlers{db: fakeHandlerStore{}}
	req := withRouteID(httptest.NewRequest(http.MethodGet, "/api/v1/drives/42", nil), "42")
	rec := httptest.NewRecorder()

	h.GetDrive(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"drive not found"`) {
		t.Fatalf("expected drive not found JSON error, got %q", rec.Body.String())
	}
}

func TestStorageBackedHandlersNilStorage(t *testing.T) {
	tests := []struct {
		name string
		path string
		hit  func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request)
	}{
		{name: "readyz", path: "/readyz", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) { h.Readyz(rec, req) }},
		{name: "list drives", path: "/api/v1/drives", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) { h.ListDrives(rec, req) }},
		{name: "get drive", path: "/api/v1/drives/7", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) {
			h.GetDrive(rec, withRouteID(req, "7"))
		}},
		{name: "drive history", path: "/api/v1/drives/7/history", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) {
			h.DriveHistory(rec, withRouteID(req, "7"))
		}},
		{name: "drive attributes", path: "/api/v1/drives/7/attributes", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) {
			h.DriveAttributes(rec, withRouteID(req, "7"))
		}},
		{name: "drive tests", path: "/api/v1/drives/7/tests?page=2&page_size=25", hit: func(h *Handlers, rec *httptest.ResponseRecorder, req *http.Request) {
			h.DriveTests(rec, withRouteID(req, "7"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handlers{logger: slog.Default()}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			tt.hit(h, rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), `"error":"storage unavailable"`) {
				t.Fatalf("expected storage unavailable error, got %q", rec.Body.String())
			}
		})
	}
}
