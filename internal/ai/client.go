package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HaikuModel is the model AI Mode calls. A named constant so bumping to a
// newer Haiku model id is a one-line change; nothing else in this package
// should hard-code a model string.
const HaikuModel = "claude-haiku-4-5"

const (
	anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion     = "2023-06-01"
)

// EvidenceFact is one label/value fact the model claims backs a
// recommendation. A fixed two-field shape (not an open map) because strict
// tool-use JSON Schema requires additionalProperties: false on every
// object — an arbitrary-key evidence map isn't expressible under strict
// mode, so evidence is a list of {label, value} pairs instead.
type EvidenceFact struct {
	Label string `json:"label"`
	Value string `json:"value"` // always a string on the wire; numeric evidence is the number's string form (e.g. "$42.10", "1,204", "37")
}

// Recommendation is one model-proposed callout, prior to grounding
// validation. Mirrors api.Insight's Category/Severity/Title/Detail so the
// frontend can reuse InsightBanner's visual language.
type Recommendation struct {
	Category string         `json:"category"`
	Severity string         `json:"severity"`
	Title    string         `json:"title"`
	Detail   string         `json:"detail"`
	Evidence []EvidenceFact `json:"evidence"`
}

// ClientError is returned for any non-2xx response from Anthropic, letting
// callers tell "bad/missing key" (401) apart from other upstream failures
// without string-matching.
type ClientError struct {
	StatusCode int
	Body       string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("anthropic api: HTTP %d: %s", e.StatusCode, e.Body)
}

// Requester is the interface Generate depends on, satisfied by *Client —
// lets tests inject a fake without a real network call.
type Requester interface {
	RequestRecommendations(ctx context.Context, bundle Context) (recs []Recommendation, insufficientData bool, err error)
}

// Client calls the Anthropic Messages API directly via net/http — no SDK
// dependency, matching infrapal-token-advisor's minimal-deps go.mod.
type Client struct {
	APIKey string
	// BaseURL overrides the Messages API endpoint, for tests. Defaults to
	// anthropicMessagesURL when empty.
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient builds a Client with a sane request timeout.
func NewClient(apiKey string) *Client {
	return &Client{APIKey: apiKey, HTTPClient: &http.Client{Timeout: 30 * time.Second}}
}

// systemPrompt is fixed across calls (no per-request interpolation) so it
// never breaks Context.Hash's meaning and is easy to audit in one place.
const systemPrompt = `You are analyzing pre-aggregated, already-computed usage
statistics for one developer's use of Claude Code, a coding assistant. You
will get a JSON object with capped, sorted rows from several cost/friction
views, some rule-based insights the app already computed, small rollup
totals, and a list of data caveats. Your job: propose concrete, actionable
recommendations to reduce cost or friction.

Follow these rules exactly:
1. Use ONLY numbers, names, and facts present in the JSON input. Never
   invent, estimate, extrapolate, or imply precision the input doesn't
   support.
2. Every recommendation's evidence array must cite facts that appear
   verbatim (same number or same name) in the input JSON. Do not paraphrase
   or round a number differently than it's given.
3. If the input has insufficient signal for any grounded recommendation,
   set insufficient_data to true and return an empty recommendations array
   — do not manufacture one to have something to say.
4. You have not been shown any prompt text, response text, tool input/
   output, file paths, or commands — only aggregate counts, costs, and
   durations. Do not speculate about what the user was working on beyond
   what the aggregate numbers show.
5. Respect the data_caveats verbatim — do not claim more precision or
   certainty than they allow.
6. Return at most 5 recommendations, ordered by likely dollar or time
   impact, via the submit_recommendations tool only.`

// recommendationsTool is the forced tool-use definition. strict: true
// guarantees the returned input validates exactly against this schema.
var recommendationsTool = map[string]any{
	"name":        "submit_recommendations",
	"description": "Submit the final, grounded list of recommendations.",
	"strict":      true,
	"input_schema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"insufficient_data": map[string]any{
				"type":        "boolean",
				"description": "true if the input lacked enough signal for any grounded recommendation",
			},
			"recommendations": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"category": map[string]any{"type": "string", "enum": []string{"efficiency", "sessions", "waste", "tools"}},
						"severity": map[string]any{"type": "string", "enum": []string{"info", "warn"}},
						"title":    map[string]any{"type": "string"},
						"detail":   map[string]any{"type": "string"},
						"evidence": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label": map[string]any{"type": "string"},
									"value": map[string]any{"type": "string"},
								},
								"required":             []string{"label", "value"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"category", "severity", "title", "detail", "evidence"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"insufficient_data", "recommendations"},
		"additionalProperties": false,
	},
}

// RequestRecommendations sends bundle as the sole user-turn content
// (JSON-marshaled) and forces the submit_recommendations tool. This is
// infrapal-token-advisor's only outbound network call, and only ever runs synchronously
// inside a request initiated in direct response to the user's own Generate
// click (POST /api/ai/recommendations).
func (c *Client) RequestRecommendations(ctx context.Context, bundle Context) ([]Recommendation, bool, error) {
	payload, err := json.Marshal(bundle)
	if err != nil {
		return nil, false, fmt.Errorf("marshal context: %w", err)
	}
	reqBody := map[string]any{
		"model":       HaikuModel,
		"max_tokens":  4096,
		"temperature": 0,
		"system":      systemPrompt,
		"tools":       []any{recommendationsTool},
		"tool_choice": map[string]any{"type": "tool", "name": "submit_recommendations"},
		"messages": []any{
			map[string]any{"role": "user", "content": string(payload)},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, false, fmt.Errorf("marshal request: %w", err)
	}

	url := c.BaseURL
	if url == "" {
		url = anthropicMessagesURL
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, false, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read anthropic response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, &ClientError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var parsed struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, false, fmt.Errorf("parse anthropic response: %w", err)
	}
	for _, block := range parsed.Content {
		if block.Type == "tool_use" && block.Name == "submit_recommendations" {
			var out struct {
				InsufficientData bool             `json:"insufficient_data"`
				Recommendations  []Recommendation `json:"recommendations"`
			}
			if err := json.Unmarshal(block.Input, &out); err != nil {
				return nil, false, fmt.Errorf("parse tool input: %w", err)
			}
			return out.Recommendations, out.InsufficientData, nil
		}
	}
	return nil, false, fmt.Errorf("anthropic response had no submit_recommendations tool_use block (stop_reason=%s)", parsed.StopReason)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}
