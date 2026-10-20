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

func TestHandleListInteractions_ExcludesZeroTokenRows(t *testing.T) {
	db := seedDB(t) // p1's one api_request has no token columns set — total_tokens is 0.
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/interactions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, r := range rows {
		if r["prompt_id"] == "p1" {
			t.Errorf("expected p1 (zero tokens) excluded from the list, got %+v", r)
		}
	}
}

func TestHandleListInteractions_SortByCost(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, success)
		VALUES ('req-p1-tok', 'p1', 's1', 1010, 'claude-3-opus', 100000, 500, 1)`) // gives p1 nonzero tokens, cheap
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 2000, 3000)`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2', 's2', 1, 2000, 500, 'expensive one')`)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, success)
		VALUES ('req-p2', 'p2', 's2', 2000, 'claude-3-opus', 9000000, 500, 1)`) // far pricier

	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/interactions?sort=cost")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0]["prompt_id"] != "p2" {
		t.Errorf("first row = %v, want p2 (the pricier one) first under sort=cost", rows[0]["prompt_id"])
	}
}
