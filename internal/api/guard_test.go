package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestGuardRejectsForeignHost(t *testing.T) {
	ts := newTestServer(t, seedDB(t))

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example:4319"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGuardAllowsLoopbackHosts(t *testing.T) {
	ts := newTestServer(t, seedDB(t))

	for _, host := range []string{"", "localhost:4319", "[::1]:4319"} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Host %q: status = %d, want 200", host, resp.StatusCode)
		}
	}
}

func TestGuardRejectsCrossOriginWrite(t *testing.T) {
	ts := newTestServer(t, seedDB(t))

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/ui/visit", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestUnknownAPIPathIsNotFound(t *testing.T) {
	srv := NewServer("127.0.0.1:0", seedDB(t), fstest.MapFS{"index.html": {Data: []byte("<html>")}})
	ts := httptest.NewServer(srv.http.Handler)
	defer ts.Close()

	for path, want := range map[string]int{"/api/nope": http.StatusNotFound, "/some/page": http.StatusOK} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}
}
