package api

import (
	"net/http"
	"strconv"
	"time"

	"diskmon/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type Handlers struct {
	db     *storage.DuckDB
	events *EventBroker
}

func NewHandlers(db *storage.DuckDB, events *EventBroker) *Handlers {
	return &Handlers{db: db, events: events}
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{Status: "ok"})
}

func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		renderError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}
	if err := h.db.Ready(r.Context()); err != nil {
		renderError(w, r, http.StatusServiceUnavailable, "not ready")
		return
	}
	render.JSON(w, r, HealthResponse{Status: "ready"})
}

func (h *Handlers) ListDrives(w http.ResponseWriter, r *http.Request) {
	items, err := h.db.ListDrives(r.Context())
	if err != nil {
		renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, items)
}

func (h *Handlers) GetDrive(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := h.db.GetDrive(r.Context(), id)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		renderError(w, r, http.StatusNotFound, "drive not found")
		return
	}
	render.JSON(w, r, augmentDriveResponse(item))
}

func (h *Handlers) DriveHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	points, err := h.db.DriveHistory(r.Context(), id, 200)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, points)
}

func (h *Handlers) DriveAttributes(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	attrs, err := h.db.DriveAttributes(r.Context(), id)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, attrs)
}

func (h *Handlers) DriveTests(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 10)
	if pageSize > 100 {
		pageSize = 100
	}
	runs, err := h.db.DriveTestRuns(r.Context(), id, page, pageSize)
	if err != nil {
		renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, runs)
}

func (h *Handlers) Events(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		renderError(w, r, http.StatusServiceUnavailable, "event stream unavailable")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		renderError(w, r, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := h.events.Subscribe()
	defer unsubscribe()

	_, _ = w.Write([]byte("retry: 5000\n\n"))
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, flusher, ev); err != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		renderError(w, r, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	render.Status(r, status)
	render.JSON(w, r, ErrorResponse{Error: message})
}
