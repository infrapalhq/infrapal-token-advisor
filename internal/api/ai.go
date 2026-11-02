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

// newAIRequester is behind a seam so tests never call a real provider.
var newAIRequester = func(p ai.Provider, apiKey string) ai.Requester { return p.New(apiKey) }

// aiStatus is what the Settings page shows. It never includes a key.
func aiStatus(cfg config.Config) map[string]any {
	p := ai.FindProvider(cfg.AIProvider)
	names := []map[string]string{}
	for _, each := range ai.Providers() {
		names = append(names, map[string]string{"id": each.ID, "name": each.Name})
	}
	return map[string]any{
		"configured":    config.ResolveAIKey(cfg, p.ID, p.KeyEnv) != "",
		"provider":      p.ID,
		"provider_name": p.Name,
		"key_hint":      p.KeyHint,
		"providers":     names,
	}
}

// handleGetAIConfig handles GET /api/ai/config — reports which provider is
// chosen and whether it has a key; the key itself is never sent to the
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
	writeJSON(w, http.StatusOK, aiStatus(cfg))
}

// handlePutAIConfig handles PUT /api/ai/config, body {"provider": "...",
// "api_key": "..."}. A provider sets the choice, and api_key sets that
// provider's key if it's present. An empty string clears the stored key.
func (s *Server) handlePutAIConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string  `json:"provider"`
		APIKey   *string `json:"api_key"`
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
	if body.Provider != "" {
		if ai.FindProvider(body.Provider).ID != body.Provider {
			writeError(w, http.StatusBadRequest, "unknown provider")
			return
		}
		cfg.AIProvider = body.Provider
	}
	if body.APIKey != nil {
		if cfg.AIKeys == nil {
			cfg.AIKeys = map[string]string{}
		}
		cfg.AIKeys[ai.FindProvider(cfg.AIProvider).ID] = strings.TrimSpace(*body.APIKey)
	}
	if err := config.Save(p, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, aiStatus(cfg))
}

// handleGetAIRecommendations handles GET /api/ai/recommendations?since= —
// cached rows only, never calls a provider. Safe on every page load.
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
	provider := ai.FindProvider(cfg.AIProvider)
	apiKey := config.ResolveAIKey(cfg, provider.ID, provider.KeyEnv)
	if apiKey == "" {
		writeError(w, http.StatusPreconditionFailed, "no "+provider.Name+" API key configured — add one in Settings first")
		return
	}

	findings, err := SafeFindings(r.Context(), s.db, body.Since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := ai.Generate(r.Context(), s.db, newAIRequester(provider, apiKey), provider.Model, body.Since, cutoffMs, haveCutoff, aiFacts(findings), body.Force)
	if err != nil {
		var ce *ai.ClientError
		if errors.As(err, &ce) {
			writeError(w, http.StatusBadGateway, provider.Name+" API error: "+ce.Error())
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

// aiCategory maps a finding onto the category enum the model answers with
// (see the tool schema in internal/ai/client.go).
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
