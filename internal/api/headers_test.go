package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSecurityHeaders_OnPagesAndAPI(t *testing.T) {
	srv := NewServer("127.0.0.1:0", seedDB(t), fstest.MapFS{"index.html": {Data: []byte("<html>")}})
	ts := httptest.NewServer(srv.http.Handler)
	defer ts.Close()
	for _, path := range []string{"/", "/sessions", "/api/status", "/api/nope"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		csp := resp.Header.Get("Content-Security-Policy")
		for _, want := range []string{"script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP %q lacks %q", path, csp, want)
			}
		}
		if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s: CSP allows inline or eval: %q", path, csp)
		}
		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", path, header, got, want)
			}
		}
	}
}

// Headers must reach a refused request too, so a page can't frame the error.
func TestSecurityHeaders_OnRefusedRequests(t *testing.T) {
	srv := NewServer("127.0.0.1:0", seedDB(t), nil)
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/api/status", nil)
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("status %d, X-Frame-Options %q", rec.Code, rec.Header().Get("X-Frame-Options"))
	}
}
