package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGetOverview_LinksTheView(t *testing.T) {
	result, rpcErr := rpc(t, newTestServer(t), "tools/list", nil)
	if rpcErr != nil {
		t.Fatalf("tools/list: %+v", rpcErr)
	}
	var got struct {
		Tools []struct {
			Name string `json:"name"`
			Meta struct {
				UI struct {
					ResourceURI string `json:"resourceUri"`
				} `json:"ui"`
			} `json:"_meta"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{}
	for _, tool := range got.Tools {
		links[tool.Name] = tool.Meta.UI.ResourceURI
	}
	if links["get_overview"] != overviewURI {
		t.Errorf("get_overview links %q, want %q", links["get_overview"], overviewURI)
	}
	if links["get_findings"] != "" {
		t.Errorf("only get_overview should link a view, get_findings links %q", links["get_findings"])
	}
}

func TestGetOverview_ReturnsStructuredContentWithoutContent(t *testing.T) {
	s := newTestServer(t).WithDashboard("http://127.0.0.1:4319")
	result, rpcErr := rpc(t, s, "tools/call", map[string]any{"name": "get_overview", "arguments": map[string]any{"since": "all"}})
	if rpcErr != nil {
		t.Fatalf("tools/call: %+v", rpcErr)
	}
	if strings.Contains(string(result), "SECRET") || strings.Contains(string(result), "echo") {
		t.Fatalf("overview leaked content: %s", result)
	}
	var got struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Structured struct {
			Range        string  `json:"range"`
			SpendUSD     float64 `json:"spend_usd"`
			Sessions     int     `json:"sessions"`
			DashboardURL string  `json:"dashboard_url"`
			ByDay        []struct {
				Day string `json:"day"`
			} `json:"by_day"`
			Models []struct {
				Model string `json:"model"`
			} `json:"models"`
			TopFindings []struct {
				Kind string `json:"kind"`
			} `json:"top_findings"`
		} `json:"structuredContent"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	o := got.Structured
	if got.IsError || o.Range != "all" || o.SpendUSD != 2.5 || o.Sessions != 1 {
		t.Errorf("unexpected overview: %s", result)
	}
	if len(o.ByDay) != 1 || len(o.Models) != 1 || o.Models[0].Model != "claude-opus" {
		t.Errorf("by_day and models should hold the one request: %s", result)
	}
	if len(o.TopFindings) == 0 {
		t.Errorf("seed should produce a finding: %s", result)
	}
	if o.DashboardURL != "http://127.0.0.1:4319" {
		t.Errorf("dashboard_url = %q", o.DashboardURL)
	}
	// The text is the same overview, for hosts that don't draw the view.
	var text map[string]any
	if len(got.Content) != 1 || json.Unmarshal([]byte(got.Content[0].Text), &text) != nil || text["range"] != "all" {
		t.Errorf("text should be the overview as JSON: %s", result)
	}
}

func TestGetOverview_RejectsBadArguments(t *testing.T) {
	s := newTestServer(t)
	if text, isError := call(t, s, "get_overview", map[string]any{"since": "nonsense"}); !isError {
		t.Errorf("bad range: want an error, got %s", text)
	}
	if text, isError := call(t, s, "get_overview", map[string]any{"since": 7}); !isError || !strings.Contains(text, "invalid arguments") {
		t.Errorf("wrong type: want invalid arguments, got %s", text)
	}
	if text, isError := call(t, s, "get_overview", nil); isError || !strings.Contains(text, `"range":"30d"`) {
		t.Errorf("no range should default to 30d: %s", text)
	}
}

func TestOtherToolsHaveNoStructuredContent(t *testing.T) {
	result, rpcErr := rpc(t, newTestServer(t), "tools/call", map[string]any{"name": "get_findings", "arguments": map[string]any{}})
	if rpcErr != nil {
		t.Fatalf("tools/call: %+v", rpcErr)
	}
	if strings.Contains(string(result), "structuredContent") {
		t.Errorf("get_findings declares no view, so it shouldn't send structuredContent: %s", result)
	}
}

func TestResources(t *testing.T) {
	s := newTestServer(t)
	if result, _ := rpc(t, s, "initialize", map[string]any{}); !strings.Contains(string(result), `"resources":{}`) {
		t.Errorf("capabilities don't include resources: %s", result)
	}
	if result, _ := rpc(t, s, "resources/list", nil); !strings.Contains(string(result), `"uri":"`+overviewURI+`"`) {
		t.Errorf("resources/list doesn't list the overview: %s", result)
	}
	if result, _ := rpc(t, s, "resources/templates/list", nil); string(result) != `{"resourceTemplates":[]}` {
		t.Errorf("resources/templates/list = %s", result)
	}

	result, rpcErr := rpc(t, s, "resources/read", map[string]any{"uri": overviewURI})
	if rpcErr != nil {
		t.Fatalf("resources/read: %+v", rpcErr)
	}
	var got struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Contents) != 1 || got.Contents[0].MimeType != "text/html;profile=mcp-app" || !strings.HasPrefix(got.Contents[0].Text, "<!doctype html>") {
		t.Fatalf("unexpected resource: %s", result)
	}
	html := got.Contents[0].Text
	for _, want := range []string{"ui/initialize", "ui/notifications/initialized", "ui/notifications/tool-result", "get_overview"} {
		if !strings.Contains(html, want) {
			t.Errorf("view doesn't use %s", want)
		}
	}
	// Hosts block external requests by default, so the view must make none.
	for _, external := range []string{"src=\"http", "href=\"http", "@import", "fetch("} {
		if strings.Contains(html, external) {
			t.Errorf("view loads something external: %s", external)
		}
	}

	for name, params := range map[string]any{
		"unknown uri": map[string]any{"uri": "ui://infrapal-token-advisor/nope.html"},
		"bad params":  "not an object",
	} {
		if _, rpcErr := rpc(t, s, "resources/read", params); rpcErr == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, rpcErr := rpc(t, s, "resources/read", map[string]any{"uri": "ui://x"}); rpcErr == nil || rpcErr.Code != codeResourceNotFound {
		t.Errorf("unknown uri: want %d, got %+v", codeResourceNotFound, rpcErr)
	}
}
