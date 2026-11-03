package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ai"
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
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["configured"] != false || got["provider_name"] != "Anthropic" {
		t.Fatalf("expected configured=false before any key is saved")
	}

	body, _ := json.Marshal(map[string]string{"api_key": "sk-ant-test-key"})
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
	var got2 map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got2["configured"] != true {
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

func TestAIFacts_SkipsFixedAndCaps(t *testing.T) {
	findings := []SafeFinding{
		{Kind: "failing_mcp", Area: "exec", Title: "fixed already", MarkedFixed: true, Severity: "full"},
		{Kind: "failing_mcp", Area: "exec", Title: "mcp", Severity: "full"},
		{Kind: "repeat_cmd", Area: "exec", Title: "cmd", Severity: "half"},
		{Kind: "re_read", Area: "context", Title: "read", Severity: "half"},
	}
	facts := aiFacts(findings)
	if len(facts) != 3 {
		t.Fatalf("facts = %+v, want the 3 unfixed findings", facts)
	}
	if f := facts[0]; f.Title != "mcp" || f.Category != "tools" || f.Severity != "warn" {
		t.Errorf("mcp fact = %+v", f)
	}
	if f := facts[1]; f.Category != "waste" || f.Severity != "info" {
		t.Errorf("exec fact = %+v", f)
	}
	if f := facts[2]; f.Category != "efficiency" {
		t.Errorf("context fact = %+v", f)
	}

	many := make([]SafeFinding, maxAIFacts+5)
	if got := len(aiFacts(many)); got != maxAIFacts {
		t.Errorf("got %d facts, want the cap of %d", got, maxAIFacts)
	}
}

// fakeRequester stands in for Anthropic and records what it was asked.
type fakeRequester struct {
	bundle ai.Context
	recs   []ai.Recommendation
	err    error
}

func (f *fakeRequester) RequestRecommendations(_ context.Context, bundle ai.Context) ([]ai.Recommendation, bool, error) {
	f.bundle = bundle
	return f.recs, false, f.err
}

func withFakeRequester(t *testing.T, f *fakeRequester) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	old := newAIRequester
	newAIRequester = func(ai.Provider, string) ai.Requester { return f }
	t.Cleanup(func() { newAIRequester = old })
}

func postAI(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/api/ai/recommendations", "application/json", strings.NewReader(`{"since":"30d"}`))
	if err != nil {
		t.Fatalf("POST /api/ai/recommendations: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out
}

func TestPostAIRecommendations_GeneratesThenServesSavedResult(t *testing.T) {
	fake := &fakeRequester{recs: []ai.Recommendation{{
		Category: "waste",
		Severity: "warn",
		Title:    "Grounded",
		Detail:   "cites a known model",
		Evidence: []ai.EvidenceFact{{Label: "model", Value: "claude-3-opus"}},
	}, {
		Category: "waste",
		Severity: "warn",
		Title:    "Made up",
		Detail:   "cites nothing we sent",
		Evidence: []ai.EvidenceFact{{Label: "cost", Value: "$123456.78"}},
	}}}
	withFakeRequester(t, fake)
	ts := newTestServer(t, seedDB(t))

	code, out := postAI(t, ts.URL)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	recs := out["recommendations"].([]any)
	if len(recs) != 1 || recs[0].(map[string]any)["title"] != "Grounded" {
		t.Errorf("recommendations = %v, want only the grounded one", recs)
	}
	if out["dropped_count"] != 1.0 {
		t.Errorf("dropped_count = %v, want 1", out["dropped_count"])
	}

	resp, err := http.Get(ts.URL + "/api/ai/recommendations?since=30d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var saved map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if saved["batch_id"] != out["batch_id"] || len(saved["recommendations"].([]any)) != 1 {
		t.Errorf("saved result = %v, want batch %v", saved, out["batch_id"])
	}
}

func TestPostAIRecommendations_AnthropicErrorIsBadGateway(t *testing.T) {
	withFakeRequester(t, &fakeRequester{err: &ai.ClientError{StatusCode: 401, Body: "invalid key"}})
	ts := newTestServer(t, seedDB(t))

	code, out := postAI(t, ts.URL)
	if code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502: %v", code, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "invalid key") {
		t.Errorf("error = %q, want the upstream reason", msg)
	}
}

func TestPutAIConfig_RefusesAnUnknownProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ts := newTestServer(t, seedDB(t))
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/ai/config", strings.NewReader(`{"provider":"no-such-provider"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPutAIConfig_KeepsTheKeyWhenOnlyChoosingAProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	ts := newTestServer(t, seedDB(t))
	put := func(body string) map[string]any {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/ai/config", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	put(`{"api_key":"key-1"}`)
	if got := put(`{"provider":"anthropic"}`); got["configured"] != true {
		t.Errorf("choosing a provider cleared the key: %v", got)
	}
	if got := put(`{"api_key":""}`); got["configured"] != false {
		t.Errorf("an empty key should clear it: %v", got)
	}
}

func TestAIConfig_NeverReturnsTheKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "secret-from-env")
	ts := newTestServer(t, seedDB(t))
	resp, err := http.Get(ts.URL + "/api/ai/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "secret-from-env") {
		t.Errorf("the key leaked: %s", buf.String())
	}
}

func TestAIEndpoints_RejectBadRequests(t *testing.T) {
	withFakeRequester(t, &fakeRequester{})
	ts := newTestServer(t, seedDB(t))
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/ai/config", strings.NewReader(`{`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("broken config body: status %d, want 400", resp.StatusCode)
	}

	resp, err = http.Post(ts.URL+"/api/ai/recommendations", "application/json", strings.NewReader(`{"since":"forever"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown range: status %d, want 400", resp.StatusCode)
	}
}

func TestPostAIRecommendations_OtherFailuresAreServerErrors(t *testing.T) {
	withFakeRequester(t, &fakeRequester{err: errors.New("connection reset")})
	ts := newTestServer(t, seedDB(t))
	code, out := postAI(t, ts.URL)
	if code != http.StatusInternalServerError || !strings.Contains(out["error"].(string), "connection reset") {
		t.Errorf("status %d, body %v", code, out)
	}
}
