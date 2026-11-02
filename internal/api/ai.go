package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ai"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/durfmt"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

// maxAIFacts caps the findings sent to the model, since every one costs tokens.
const maxAIFacts = 20

// handleGetAIConfig handles GET /api/ai/config — reports only whether an
// Anthropic API key is configured; the key itself is never sent to the
// frontend.
func (s *Server) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	p, err := paths.Resolve()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := config.Load(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": config.ResolveAnthropicAPIKey(cfg) != ""})
}

// handlePutAIConfig handles PUT /api/ai/config, body
// {"anthropic_api_key": "sk-ant-..."}. Sending an empty string clears the
// stored key.
func (s *Server) handlePutAIConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnthropicAPIKey string `json:"anthropic_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	p, err := paths.Resolve()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := config.Load(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg.AnthropicAPIKey = strings.TrimSpace(body.AnthropicAPIKey)
	if err := config.Save(p, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": cfg.AnthropicAPIKey != ""})
}

// handleGetAIRecommendations handles GET /api/ai/recommendations?since= —
// cached rows only, never calls Anthropic. Safe on every page load.
func (s *Server) handleGetAIRecommendations(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	result, err := ai.GetCached(r.Context(), s.db, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handlePostAIRecommendations handles POST /api/ai/recommendations, body
// {"since": "30d", "force": false}. This is one of infrapal-token-advisor's two deliberate
// exceptions to read-only/local-only (see the package doc) — it runs only
// in direct response to a user-triggered request.
func (s *Server) handlePostAIRecommendations(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Since string `json:"since"`
		Force bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // empty body -> zero values, both fine

	var cutoffMs int64
	var haveCutoff bool
	if body.Since != "" {
		d, err := durfmt.ParseSince(body.Since)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cutoffMs = time.Now().Add(-d).UnixMilli()
		haveCutoff = true
	}

	p, err := paths.Resolve()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, err := config.Load(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	apiKey := config.ResolveAnthropicAPIKey(cfg)
	if apiKey == "" {
		writeError(w, http.StatusPreconditionFailed, "no Anthropic API key configured — add one in Settings first")
		return
	}

	findings, err := SafeFindings(r.Context(), s.db, body.Since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	client := ai.NewClient(apiKey)
	result, err := ai.Generate(r.Context(), s.db, client, body.Since, cutoffMs, haveCutoff, aiFacts(findings), body.Force)
	if err != nil {
		var ce *ai.ClientError
		if errors.As(err, &ce) {
			writeError(w, http.StatusBadGateway, "Anthropic API error: "+ce.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// aiFacts turns findings into what the model sees, highest priority first.
// They are SafeFindings, so no file path, command, or project name goes out.
func aiFacts(findings []SafeFinding) []ai.InsightFact {
	facts := make([]ai.InsightFact, 0, maxAIFacts)
	for _, f := range findings {
		if f.MarkedFixed {
			continue
		}
		if len(facts) == maxAIFacts {
			break
		}
		severity := "info"
		if f.Severity == "full" {
			severity = "warn"
		}
		facts = append(facts, ai.InsightFact{
			Category: aiCategory(f),
			Severity: severity,
			Title:    f.Title,
			Detail:   f.Detail,
		})
	}
	return facts
}

// aiCategory maps a finding onto the category enum in the Anthropic tool
// schema (internal/ai/client.go).
func aiCategory(f SafeFinding) string {
	switch {
	case strings.Contains(f.Kind, "mcp"):
		return "tools"
	case f.Area == "exec" || f.Area == "perm":
		return "waste"
	default:
		return "efficiency"
	}
}
