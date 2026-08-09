package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleListSessions(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var sessions []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0]["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", sessions[0]["session_id"])
	}
	if sessions[0]["total_cost_usd"] != 0.1 {
		t.Errorf("total_cost_usd = %v, want 0.1", sessions[0]["total_cost_usd"])
	}
}

func TestHandleListSessions_Empty(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`DELETE FROM sessions`); err != nil {
		t.Fatalf("delete sessions: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var sessions []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestHandleGetSession(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/sessions/s1")
	if err != nil {
		t.Fatalf("GET /api/sessions/s1: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Session      map[string]any   `json:"session"`
		Interactions []map[string]any `json:"interactions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", body.Session["session_id"])
	}
	if len(body.Interactions) != 1 || body.Interactions[0]["prompt_id"] != "p1" {
		t.Errorf("interactions = %+v, want one row with prompt_id p1", body.Interactions)
	}
}

func TestHandleGetSession_NotFound(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/sessions/does-not-exist")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
