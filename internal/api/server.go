// Package api implements infrapal-token-advisor's HTTP API over the captured
// telemetry, serving the dashboard: insights, session timelines, and fixes.
//
// The API only ever talks to this machine, with one exception:
// POST /api/ai/recommendations calls a model provider, but only in direct
// response to a user's request, and only once a key has been configured. See
// internal/ai and the README's network note.
package api

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net"
	"net/http"

	"github.com/infrapalhq/infrapal-token-advisor/internal/localhttp"
	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
)

// Server is infrapal-token-advisor's local HTTP API, read-only over the SQLite database.
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
	mux.HandleFunc("GET /api/ai/config", s.handleGetAIConfig)
	mux.HandleFunc("PUT /api/ai/config", s.handlePutAIConfig)
	mux.HandleFunc("GET /api/ai/recommendations", s.handleGetAIRecommendations)
	mux.HandleFunc("POST /api/ai/recommendations", s.handlePostAIRecommendations)
	mux.HandleFunc("GET /api/agents", s.handleAgents)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/sessions/cards", s.handleSessionCards)
	mux.HandleFunc("GET /api/sessions/{id}/timeline", s.handleSessionTimeline)
	mux.HandleFunc("GET /api/sessions/{id}/issues", s.handleSessionIssues)
	mux.HandleFunc("GET /api/fixes", s.handleFixes)
	mux.HandleFunc("POST /api/fixes/{id}/state", s.handlePostFixState)
	mux.HandleFunc("POST /api/ui/visit", s.handlePostUIVisit)
	mux.HandleFunc("GET /api/progress", s.handleProgress)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/mcp/setup", s.handleMCPSetup)
	if uiFS != nil {
		mux.Handle("/", newSPAHandler(uiFS))
	}
	s.http = localhttp.Server(addr, logging.RecoverHTTP("api", securityHeaders(guard(mux))))
	s.http.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn)
	return s
}

// Endpoints lists every route this server exposes, for printing to the user
// (e.g. from `token-advisor ui`).
func Endpoints() []string {
	return []string{
		"GET /api/ai/config",
		"PUT /api/ai/config",
		"GET /api/ai/recommendations?since=",
		"POST /api/ai/recommendations",
		"GET /api/agents",
		"GET /api/overview?since=&agent=",
		"GET /api/sessions/cards?since=&agent=",
		"GET /api/sessions/{id}/timeline",
		"GET /api/sessions/{id}/issues",
		"GET /api/fixes?since=&agent=",
		"POST /api/fixes/{id}/state",
		"POST /api/ui/visit",
		"GET /api/progress?since=&agent=",
		"GET /api/status",
		"GET /api/mcp/setup",
	}
}

// ListenAndServe blocks serving until the server is shut down, mirroring
// http.Server.ListenAndServe (returns http.ErrServerClosed on clean
// shutdown).
func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

// Serve is ListenAndServe over a listener the caller already bound, so a port
// conflict can be handled before serving starts.
func (s *Server) Serve(l net.Listener) error {
	return s.http.Serve(l)
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
