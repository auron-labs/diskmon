package api

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"diskmon/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(logger *slog.Logger, db *storage.DuckDB, events *EventBroker, staticFS fs.FS, apiKey string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	h := NewHandlers(logger, db, events)
	r.Get("/healthz", h.Healthz)
	r.Get("/readyz", h.Readyz)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(apiKeyAuth(apiKey))
		api.Get("/drives", h.ListDrives)
		api.Get("/drives/{id}", h.GetDrive)
		api.Get("/drives/{id}/history", h.DriveHistory)
		api.Get("/drives/{id}/attributes", h.DriveAttributes)
		api.Get("/drives/{id}/tests", h.DriveTests)
		api.Get("/events", h.Events)
	})

	if staticFS != nil {
		assets, err := fs.Sub(staticFS, "assets")
		if err == nil {
			r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
		}

		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/api/") {
				http.NotFound(w, req)
				return
			}

			f, err := staticFS.Open("index.html")
			if err != nil {
				http.NotFound(w, req)
				return
			}
			defer f.Close()

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.Copy(w, f)
		})
	}

	return r
}
