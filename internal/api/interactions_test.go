package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleGetInteraction(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/interactions/p1")
	if err != nil {
		t.Fatalf("GET /api/interactions/p1: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Interaction map[string]any   `json:"interaction"`
		Spans       []map[string]any `json:"spans"`
		APIRequests []map[string]any `json:"api_requests"`
		ToolCalls   []map[string]any `json:"tool_calls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Interaction["prompt_id"] != "p1" {
		t.Errorf("interaction.prompt_id = %v, want p1", body.Interaction["prompt_id"])
	}
	if len(body.Spans) != 1 || body.Spans[0]["span_id"] != "sp-1" {
		t.Errorf("spans = %+v, want one row with span_id sp-1", body.Spans)
	}
	if len(body.APIRequests) != 1 || body.APIRequests[0]["request_id"] != "req-1" {
		t.Errorf("api_requests = %+v, want one row with request_id req-1", body.APIRequests)
	}
	if len(body.ToolCalls) != 1 || body.ToolCalls[0]["tool_use_id"] != "tu-1" {
		t.Errorf("tool_calls = %+v, want one row with tool_use_id tu-1", body.ToolCalls)
	}
}

func TestHandleGetInteraction_NotFound(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/interactions/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
