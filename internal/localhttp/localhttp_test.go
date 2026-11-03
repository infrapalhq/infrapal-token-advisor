package localhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := Guard(ok)
	cases := []struct {
		name   string
		host   string
		header map[string]string
		method string
		want   int
	}{
		{"local read", "127.0.0.1:4318", nil, http.MethodGet, http.StatusNoContent},
		{"local write", "localhost:4318", nil, http.MethodPost, http.StatusNoContent},
		{"rebound domain", "evil.example:4318", nil, http.MethodPost, http.StatusForbidden},
		{"another site's page", "127.0.0.1:4318", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.MethodPost, http.StatusForbidden},
		{"another origin", "127.0.0.1:4318", map[string]string{"Origin": "https://evil.example"}, http.MethodPost, http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, "http://"+c.host+"/", nil)
		req.Host = c.host
		for k, v := range c.header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestServer_HasTimeouts(t *testing.T) {
	s := Server("127.0.0.1:0", http.NotFoundHandler())
	if s.ReadHeaderTimeout == 0 || s.IdleTimeout == 0 {
		t.Errorf("timeouts = %v, %v", s.ReadHeaderTimeout, s.IdleTimeout)
	}
}
