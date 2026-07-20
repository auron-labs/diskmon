package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"diskmon/internal/storage"
	"diskmon/web"
)

type Server struct {
	http   *http.Server
	log    *slog.Logger
	events *EventBroker
}

func NewServer(addr string, apiKey string, logger *slog.Logger, db *storage.DuckDB, events *EventBroker) *Server {
	handler := NewRouter(logger, db, events, web.Assets(), apiKey)
	return &Server{
		log:    logger,
		events: events,
		http: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
	}
}

func (s *Server) Start() error {
	s.log.Info("api server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("api server shutting down")
	if s.events != nil {
		s.events.Close()
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) Close() error {
	if s.events != nil {
		s.events.Close()
	}
	return s.http.Close()
}
