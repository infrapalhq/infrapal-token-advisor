package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestHandleSearch(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/search?q=" + url.QueryEscape("hello"))
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if results[0]["kind"] != "prompt" {
		t.Errorf("kind = %v, want prompt", results[0]["kind"])
	}
	if results[0]["id"] != "p1" {
		t.Errorf("id = %v, want p1", results[0]["id"])
	}
}

func TestHandleSearch_MatchesResponseText(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/search?q=" + url.QueryEscape("hi there"))
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 || results[0]["kind"] != "response" {
		t.Fatalf("results = %+v, want one row with kind response", results)
	}
	if results[0]["id"] != "m1" {
		t.Errorf("id = %v, want m1", results[0]["id"])
	}
}

func TestHandleSearch_NoMatch(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/search?q=" + url.QueryEscape("nonexistentterm"))
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestHandleSearch_MissingQuery(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/search")
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleSearch_OperatorCharacters exercises the FTS5-phrase-escaping
// path: a raw FTS5 query containing operator characters like "-" or ":"
// would otherwise raise a syntax error instead of just finding no match.
func TestHandleSearch_OperatorCharacters(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/search?q=" + url.QueryEscape(`foo -bar "baz`))
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (operator chars must not 500)", resp.StatusCode)
	}
}
