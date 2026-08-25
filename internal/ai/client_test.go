package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_RequestRecommendations_ForcesToolChoiceAndParsesResponse(t *testing.T) {
	var capturedReq map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{
					"type": "tool_use",
					"name": "submit_recommendations",
					"input": map[string]any{
						"insufficient_data": false,
						"recommendations": []map[string]any{
							{
								"category": "waste",
								"severity": "warn",
								"title":    "Example",
								"detail":   "detail text",
								"evidence": []map[string]any{{"label": "x", "value": "1"}},
							},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := &Client{APIKey: "test-key", BaseURL: ts.URL}
	recs, insufficient, err := client.RequestRecommendations(context.Background(), Context{})
	if err != nil {
		t.Fatalf("RequestRecommendations: %v", err)
	}
	if insufficient {
		t.Errorf("insufficientData = true, want false")
	}
	if len(recs) != 1 || recs[0].Title != "Example" {
		t.Fatalf("recs = %+v, want one recommendation titled Example", recs)
	}

	toolChoice, _ := capturedReq["tool_choice"].(map[string]any)
	if toolChoice["type"] != "tool" || toolChoice["name"] != "submit_recommendations" {
		t.Errorf("tool_choice = %+v, want forced submit_recommendations", toolChoice)
	}
	tools, _ := capturedReq["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected exactly one tool sent, got %+v", tools)
	}
	toolDef, _ := tools[0].(map[string]any)
	if strict, _ := toolDef["strict"].(bool); !strict {
		t.Errorf("tool strict = %v, want true", toolDef["strict"])
	}
}

func TestClient_RequestRecommendations_NonOKStatusReturnsClientError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
	}))
	defer ts.Close()

	client := &Client{APIKey: "bad-key", BaseURL: ts.URL}
	_, _, err := client.RequestRecommendations(context.Background(), Context{})
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
	ce, ok := err.(*ClientError)
	if !ok {
		t.Fatalf("err = %T, want *ClientError", err)
	}
	if ce.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", ce.StatusCode)
	}
}

func TestClient_RequestRecommendations_InsufficientDataReturnsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{
					"type": "tool_use",
					"name": "submit_recommendations",
					"input": map[string]any{
						"insufficient_data": true,
						"recommendations":   []map[string]any{},
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := &Client{APIKey: "test-key", BaseURL: ts.URL}
	recs, insufficient, err := client.RequestRecommendations(context.Background(), Context{})
	if err != nil {
		t.Fatalf("RequestRecommendations: %v", err)
	}
	if !insufficient {
		t.Errorf("insufficientData = false, want true")
	}
	if len(recs) != 0 {
		t.Errorf("recs = %+v, want empty", recs)
	}
}
