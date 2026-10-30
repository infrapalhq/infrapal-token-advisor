// Package api implements infrapal-token-advisor's HTTP API over the captured telemetry:
// sessions, interaction waterfalls, full-text search, and the derived views
// from migration 0006. Business logic lives in SQL (the views); handlers
// here stay thin — decode params, run one query, encode JSON.
//
// Two deliberate exceptions to "read-only, local-only":
//
//   - POST /api/ai/recommendations makes an outbound call to the Anthropic
//     API, but only in direct response to a user-triggered request from
//     the Trace page's "Explain this trace" action, and only once an API
//     key has been explicitly configured — see internal/ai and the
//     README's network-egress note.
//   - POST /api/findings/{id}/state is this API's first write endpoint: it
//     records a Dismiss/Snooze/Mark-fixed decision against the local
//     finding_state table (migration 0009). Nothing here leaves the
//     machine.
package api

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net"
	"net/http"

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
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /api/interactions", s.handleListInteractions)
	mux.HandleFunc("GET /api/interactions/{prompt_id}", s.handleGetInteraction)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/views/{name}", s.handleView)
	mux.HandleFunc("GET /api/timeseries", s.handleTimeseries)
	mux.HandleFunc("GET /api/findings", s.handleFindings)
	mux.HandleFunc("POST /api/findings/{id}/state", s.handlePostFindingState)
	mux.HandleFunc("GET /api/findings/activity", s.handleFindingsActivity)
	mux.HandleFunc("GET /api/ai/config", s.handleGetAIConfig)
	mux.HandleFunc("PUT /api/ai/config", s.handlePutAIConfig)
	mux.HandleFunc("GET /api/ai/recommendations", s.handleGetAIRecommendations)
	mux.HandleFunc("POST /api/ai/recommendations", s.handlePostAIRecommendations)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/sessions/cards", s.handleSessionCards)
	mux.HandleFunc("GET /api/sessions/{id}/timeline", s.handleSessionTimeline)
	mux.HandleFunc("GET /api/sessions/{id}/issues", s.handleSessionIssues)
	mux.HandleFunc("GET /api/fixes", s.handleFixes)
	mux.HandleFunc("POST /api/fixes/{id}/state", s.handlePostFixState)
	mux.HandleFunc("GET /api/ui/state", s.handleGetUIState)
	mux.HandleFunc("POST /api/ui/visit", s.handlePostUIVisit)
	mux.HandleFunc("GET /api/progress", s.handleProgress)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	if uiFS != nil {
		mux.Handle("/", newSPAHandler(uiFS))
	}
	s.http = &http.Server{
		Addr:     addr,
		Handler:  logging.RecoverHTTP("api", guard(mux)),
		ErrorLog: slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	return s
}

// Endpoints lists every route this server exposes, for printing to the user
// (e.g. from `token-advisor ui`).
func Endpoints() []string {
	return []string{
		"GET /api/sessions",
		"GET /api/sessions/{id}",
		"GET /api/interactions?limit=&offset=&since=&sort=",
		"GET /api/interactions/{prompt_id}",
		"GET /api/search?q=",
		"GET /api/views/{name}",
		"GET /api/timeseries?metric=&bucket=",
		"GET /api/findings",
		"POST /api/findings/{id}/state",
		"GET /api/findings/activity?since=",
		"GET /api/ai/config",
		"PUT /api/ai/config",
		"GET /api/ai/recommendations?since=",
		"POST /api/ai/recommendations",
		"GET /api/overview?since=",
		"GET /api/sessions/cards?since=",
		"GET /api/sessions/{id}/timeline",
		"GET /api/sessions/{id}/issues",
		"GET /api/fixes?since=",
		"POST /api/fixes/{id}/state",
		"GET /api/ui/state",
		"POST /api/ui/visit",
		"GET /api/progress?since=",
		"GET /api/status",
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
