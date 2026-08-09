// Package api implements ccotel's read-only HTTP API over the captured
// telemetry: sessions, interaction waterfalls, full-text search, and the
// derived views from migration 0006. Business logic lives in SQL (the
// views); handlers here stay thin — decode params, run one query, encode
// JSON.
package api

import (
	"context"
	"database/sql"
	"io/fs"
	"net/http"
)

// Server is ccotel's local HTTP API, read-only over the SQLite database.
type Server struct {
	db   *sql.DB
	http *http.Server
}

// NewServer builds a Server bound to addr (must be a 127.0.0.1 address —
// this API is never meant to see traffic beyond localhost). uiFS, if
// non-nil, is mounted as a catch-all "/" SPA handler serving the embedded
// dashboard (see internal/webui) alongside the /api/* routes — Go's
// ServeMux matches the more specific /api/* patterns first, so the two
// coexist safely. Pass nil to run API-only (e.g. in tests).
func NewServer(addr string, db *sql.DB, uiFS fs.FS) *Server {
	s := &Server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /api/interactions", s.handleListInteractions)
	mux.HandleFunc("GET /api/interactions/{prompt_id}", s.handleGetInteraction)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/views/{name}", s.handleView)
	mux.HandleFunc("GET /api/timeseries", s.handleTimeseries)
	if uiFS != nil {
		mux.Handle("/", newSPAHandler(uiFS))
	}
	s.http = &http.Server{Addr: addr, Handler: mux}
	return s
}

// Endpoints lists every route this server exposes, for printing to the user
// (e.g. from `ccotel ui`).
func Endpoints() []string {
	return []string{
		"GET /api/sessions",
		"GET /api/sessions/{id}",
		"GET /api/interactions?limit=&offset=",
		"GET /api/interactions/{prompt_id}",
		"GET /api/search?q=",
		"GET /api/views/{name}",
		"GET /api/timeseries?metric=&bucket=",
	}
}

// ListenAndServe blocks serving until the server is shut down, mirroring
// http.Server.ListenAndServe (returns http.ErrServerClosed on clean
// shutdown).
func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
