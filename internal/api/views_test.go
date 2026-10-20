package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestHandleView_AllNames exercises every whitelisted view name against the
// shared fixture, confirming the pass-through query executes cleanly and
// returns valid JSON. v_wasted_spend needs a rejected tool call,
// v_retry_waste needs a retried api_request, and v_ancillary_model_spend
// needs an api_request on one of its specific query_source values — none
// of which the shared fixture has (its tool_call is 'accept', its one
// api_request has no attempt column set, and its query_source is unset),
// so those three get their own tests with an extra seeded row each.
func TestHandleView_AllNames(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	for name := range viewTable {
		if name == "wasted_spend" || name == "retry_waste" || name == "ancillary_model_spend" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/views/" + name)
			if err != nil {
				t.Fatalf("GET /api/views/%s: %v", name, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var rows []map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(rows) == 0 {
				t.Errorf("expected at least 1 row for view %s, got 0", name)
			}
		})
	}
}

func TestHandleView_PromptEfficiency_Values(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/prompt_efficiency")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["prompt_id"] != "p1" {
		t.Errorf("prompt_id = %v, want p1", rows[0]["prompt_id"])
	}
	if rows[0]["tool_call_count"] != float64(1) {
		t.Errorf("tool_call_count = %v, want 1", rows[0]["tool_call_count"])
	}
}

func TestHandleView_WastedSpend(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`
		INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-2', 'p1', 's1', 1060, 'Bash', 'reject', 1)
	`); err != nil {
		t.Fatalf("seed reject: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/wasted_spend")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0]["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", rows[0]["session_id"])
	}
	if rows[0]["rejected_tool_count"] != float64(1) {
		t.Errorf("rejected_tool_count = %v, want 1", rows[0]["rejected_tool_count"])
	}
}

func TestHandleView_RetryWaste(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`
		INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, attempt, success)
		VALUES ('req-2', 'p1', 's1', 1070, 'claude-3-opus', 50000, 2, 1)
	`); err != nil {
		t.Fatalf("seed retry: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/retry_waste")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0]["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", rows[0]["session_id"])
	}
	if rows[0]["retried_requests"] != float64(1) {
		t.Errorf("retried_requests = %v, want 1", rows[0]["retried_requests"])
	}
}

func TestHandleView_AncillaryModelSpend(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`
		INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, query_source, cost_usd_micros, input_tokens, success)
		VALUES ('req-mm', 'p1', 's1', 1070, 'claude-sonnet-5', 'away_summary', 600000, 1000, 1)
	`); err != nil {
		t.Fatalf("seed ancillary model spend: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/ancillary_model_spend")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0]["query_source"] != "away_summary" {
		t.Errorf("query_source = %v, want away_summary", rows[0]["query_source"])
	}
	if rows[0]["cost_usd"] != 0.6 {
		t.Errorf("cost_usd = %v, want 0.6", rows[0]["cost_usd"])
	}
}

func TestHandleView_UnknownName(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/does_not_exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleView_SessionIDFilter(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`
		INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 2000, 6000)
	`); err != nil {
		t.Fatalf("seed s2: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/session_summary?session_id=s1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0]["session_id"] != "s1" {
		t.Fatalf("rows = %+v, want exactly one row for s1", rows)
	}
}

// TestHandleView_SessionIDFilter_IgnoredForViewsWithoutIt confirms
// ?session_id= is silently ignored (not an error) on views that don't carry
// a session_id column, per viewsWithSessionID.
func TestHandleView_SessionIDFilter_IgnoredForViewsWithoutIt(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/views/mcp_inventory?session_id=s1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
