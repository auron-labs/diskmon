package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"diskmon/internal/storage"

	"github.com/go-chi/chi/v5"
)

type fakeHandlerStore struct {
	readyErr      error
	listDrivesErr error
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
	return nil, f.getDriveErr
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
