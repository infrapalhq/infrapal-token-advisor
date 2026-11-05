package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// If the database fails under a running dashboard, every endpoint should say
// so with a server error, never crash or answer as if there were no data.
func TestEndpoints_ReportDatabaseFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db := seedDB(t)
	ts := newTestServer(t, db)
	db.Close()

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/agents"},
		{"GET", "/api/overview?since=all"},
		{"GET", "/api/sessions/cards?since=all"},
		{"GET", "/api/sessions/s1/timeline"},
		{"GET", "/api/sessions/s1/issues"},
		{"GET", "/api/fixes?since=all"},
		{"POST", "/api/fixes/re_read%7Cx/state"},
		{"POST", "/api/ui/visit"},
		{"GET", "/api/progress?since=all"},
		{"GET", "/api/status"},
	} {
		body := strings.NewReader(`{"state":"fixed"}`)
		req, err := http.NewRequest(c.method, ts.URL+c.path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s: status %d, want 500", c.method, c.path, resp.StatusCode)
		}
	}
}

// Each table the detectors read: losing any one must surface as an error,
// not as a page that quietly shows less.
func TestComputeInsights_ReportsEachMissingTable(t *testing.T) {
	for _, name := range []string{
		"sessions", "interactions", "tool_calls", "api_requests", "compactions",
		"assistant_responses", "v_user_waits", "v_mcp_inventory",
	} {
		t.Run(name, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			kind := "TABLE"
			if strings.HasPrefix(name, "v_") {
				kind = "VIEW"
			}
			if _, err := db.Exec("DROP " + kind + " " + name); err != nil {
				t.Fatal(err)
			}
			d, occ, err := computeInsights(context.Background(), db, 0, "")
			if err != nil {
				return
			}
			if _, err := buildCards(context.Background(), db, d, occ, 0); err == nil {
				t.Errorf("no error with %s missing", name)
			}
		})
	}
}

// Losing the fix-marks table must surface as an error on every endpoint that
// reads it, not as a list that quietly shows marked fixes as open.
func TestEndpoints_ReportMissingFixMarks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db := seedDB(t)
	if _, err := db.Exec(`DROP TABLE fix_marks`); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, db)

	for _, path := range []string{"/api/overview?since=all", "/api/sessions/cards?since=all", "/api/fixes?since=all"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s: status %d, want 500", path, resp.StatusCode)
		}
	}
}
