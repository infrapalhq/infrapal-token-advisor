package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// rpc sends one request through the full JSON-RPC path and returns the raw
// result or error.
func rpc(t *testing.T, s *Server, method string, params any) (json.RawMessage, *rpcError) {
	t.Helper()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := s.Serve(context.Background(), bytes.NewReader(append(req, '\n')), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	return resp.Result, resp.Error
}

func promptText(t *testing.T, s *Server, name string, args map[string]string) string {
	t.Helper()
	result, rpcErr := rpc(t, s, "prompts/get", map[string]any{"name": name, "arguments": args})
	if rpcErr != nil {
		t.Fatalf("prompts/get %s: %+v", name, rpcErr)
	}
	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Messages[0].Content.Type != "text" {
		t.Fatalf("want one user text message, got %s", result)
	}
	return got.Messages[0].Content.Text
}

func TestInitialize_AdvertisesPrompts(t *testing.T) {
	result, rpcErr := rpc(t, newTestServer(t), "initialize", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("initialize: %+v", rpcErr)
	}
	if !strings.Contains(string(result), `"prompts":{}`) {
		t.Errorf("capabilities don't include prompts: %s", result)
	}
}

func TestPromptsList(t *testing.T) {
	result, rpcErr := rpc(t, newTestServer(t), "prompts/list", nil)
	if rpcErr != nil {
		t.Fatalf("prompts/list: %+v", rpcErr)
	}
	var got struct {
		Prompts []struct {
			Name      string `json:"name"`
			Arguments []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got.Prompts {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "review,session,compare" {
		t.Errorf("prompts = %v", names)
	}
	if a := got.Prompts[1].Arguments; len(a) != 1 || a[0].Name != "session_id" || !a[0].Required {
		t.Errorf("session's arguments = %+v, want a required session_id", a)
	}
}

func TestPromptsGet_FillsInArguments(t *testing.T) {
	s := newTestServer(t)
	if text := promptText(t, s, "review", nil); !strings.Contains(text, `get_findings (since "30d")`) {
		t.Errorf("review without since should default to 30d: %s", text)
	}
	if text := promptText(t, s, "review", map[string]string{"since": " 7d "}); !strings.Contains(text, `get_findings (since "7d")`) {
		t.Errorf("review should use the trimmed since: %s", text)
	}
	if text := promptText(t, s, "session", map[string]string{"session_id": "s1"}); !strings.Contains(text, `session "s1"`) {
		t.Errorf("session should name the session: %s", text)
	}
	for _, p := range prompts {
		args := map[string]string{"session_id": "s1"}
		if text := promptText(t, s, p.Name, args); !strings.Contains(text, answerRules) {
			t.Errorf("%s doesn't end with the shared answer rules", p.Name)
		}
	}
}

func TestPromptsGet_Rejects(t *testing.T) {
	s := newTestServer(t)
	for name, params := range map[string]any{
		"unknown prompt":   map[string]any{"name": "nope"},
		"missing argument": map[string]any{"name": "session", "arguments": map[string]string{"session_id": "  "}},
		"bad params":       "not an object",
	} {
		if _, rpcErr := rpc(t, s, "prompts/get", params); rpcErr == nil || rpcErr.Code != codeInvalidParams {
			t.Errorf("%s: want invalid params, got %+v", name, rpcErr)
		}
	}
}
