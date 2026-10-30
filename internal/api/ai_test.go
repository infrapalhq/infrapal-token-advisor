package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAIConfig_GetPutRoundTrip(t *testing.T) {
	// Isolate ~/.infrapal-token-advisor so this test doesn't touch the real machine's config.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/ai/config")
	if err != nil {
		t.Fatalf("GET /api/ai/config: %v", err)
	}
	defer resp.Body.Close()
	var got map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["configured"] {
		t.Fatalf("expected configured=false before any key is saved")
	}

	body, _ := json.Marshal(map[string]string{"anthropic_api_key": "sk-ant-test-key"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/ai/config", bytes.NewReader(body))
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/ai/config: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putResp.StatusCode)
	}

	resp2, err := http.Get(ts.URL + "/api/ai/config")
	if err != nil {
		t.Fatalf("GET /api/ai/config: %v", err)
	}
	defer resp2.Body.Close()
	var got2 map[string]bool
	if err := json.NewDecoder(resp2.Body).Decode(&got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got2["configured"] {
		t.Fatalf("expected configured=true after saving a key")
	}
}

func TestPostAIRecommendations_NoKeyConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Post(ts.URL+"/api/ai/recommendations", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /api/ai/recommendations: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412", resp.StatusCode)
	}
}

func TestGetAIRecommendations_EmptyWhenNeverGenerated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/ai/recommendations?since=30d")
	if err != nil {
		t.Fatalf("GET /api/ai/recommendations: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Recommendations []any `json:"recommendations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Recommendations) != 0 {
		t.Errorf("expected no cached recommendations, got %+v", body.Recommendations)
	}
}
