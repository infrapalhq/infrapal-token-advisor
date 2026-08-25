package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ShamikMukhopadhyay/ccotel/internal/ai"
	"github.com/ShamikMukhopadhyay/ccotel/internal/config"
	"github.com/ShamikMukhopadhyay/ccotel/internal/durfmt"
	"github.com/ShamikMukhopadhyay/ccotel/internal/paths"
)

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
// {"since": "30d", "force": false}. This is ccotel's one deliberate
// network-egress point — it runs only in direct response to this request,
// which the frontend only ever sends from the AI Mode page's explicit
// "Generate" button.
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
		writeError(w, http.StatusPreconditionFailed, "no Anthropic API key configured — add one on the AI Mode page first")
		return
	}

	rawInsights, err := computeInsights(r.Context(), s.db, cutoffMs, haveCutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	facts := make([]ai.InsightFact, len(rawInsights))
	for i, ins := range rawInsights {
		facts[i] = ai.InsightFact{Category: ins.Category, Severity: ins.Severity, Title: ins.Title, Detail: ins.Detail}
	}

	client := ai.NewClient(apiKey)
	result, err := ai.Generate(r.Context(), s.db, client, body.Since, cutoffMs, haveCutoff, facts, body.Force)
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
