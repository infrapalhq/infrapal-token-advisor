package mcpserver

import (
	"encoding/json"
	"strings"
)

// prompt is one MCP prompt. Claude Code lists each as a slash command, such as
// /infrapal-token-advisor:review, so a user can start a common question
// without knowing which tools or views to ask for.
type prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Arguments   []promptArgument `json:"arguments,omitempty"`

	text func(args map[string]string) string
}

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Shared closing instructions, so every prompt's answer is grounded and
// actionable in the same way.
const answerRules = "Base every claim on numbers from the tools, and quote them. " +
	"For each suggestion, say what to change (a settings.json snippet, a permission rule, a model choice or a habit), " +
	"and what it would have saved over the period. " +
	"File paths and commands are withheld from you, so where a finding needs them, point the user to the Fixes page of the dashboard. " +
	"If there isn't enough data to say, say so rather than guessing."

var prompts = []prompt{
	{
		Name:        "review",
		Title:       "Review my usage",
		Description: "Find where tokens and money went, and what to change first.",
		Arguments: []promptArgument{{
			Name:        "since",
			Description: `Time range: "today", "7d", "30d", "3m", "install", or "all". Defaults to "30d".`,
		}},
		text: func(args map[string]string) string {
			since := args["since"]
			if since == "" {
				since = "30d"
			}
			return "Review my coding agent usage over the range \"" + since + "\" using the infrapal-token-advisor tools. " +
				"Start with get_findings (since \"" + since + "\"), then call get_schema and use run_sql to check the biggest findings " +
				"and look for anything the findings missed, such as an expensive model doing small jobs, low cache reuse or long retries. " +
				"Give me the five changes that would save the most, biggest first. " + answerRules
		},
	},
	{
		Name:        "session",
		Title:       "Explain a session",
		Description: "Explain why one session cost what it did.",
		Arguments: []promptArgument{{
			Name:        "session_id",
			Description: "The session's ID, as shown on the dashboard's Sessions page.",
			Required:    true,
		}},
		text: func(args map[string]string) string {
			return "Explain why the session \"" + args["session_id"] + "\" cost what it did, using the infrapal-token-advisor tools. " +
				"Call get_schema, then use run_sql on v_session_summary, v_api_request_detail and v_tool_call_detail for that session_id. " +
				"Show which prompts and tools cost the most, how well the cache was reused, and any retries, compactions or waits for approval. " +
				"Then say what would have made it cheaper. " + answerRules
		},
	},
	{
		Name:        "compare",
		Title:       "Compare weeks",
		Description: "Compare the last 7 days with the 7 days before, and explain what changed.",
		text: func(map[string]string) string {
			return "Compare my coding agent usage over the last 7 days with the 7 days before, using the infrapal-token-advisor tools. " +
				"Call get_schema, then use run_sql to compare spend, tokens, cache reuse, sessions and spend by model between the two weeks. " +
				"Explain the biggest changes and what caused them, and say whether anything got worse that's worth fixing. " + answerRules
		},
	},
}

// getPrompt returns a prompt's messages, filled in with its arguments.
func getPrompt(params json.RawMessage) (any, *rpcError) {
	var in struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &rpcError{codeInvalidParams, "invalid params"}
	}
	for _, p := range prompts {
		if p.Name != in.Name {
			continue
		}
		args := map[string]string{}
		for _, a := range p.Arguments {
			value := strings.TrimSpace(in.Arguments[a.Name])
			if a.Required && value == "" {
				return nil, &rpcError{codeInvalidParams, "missing argument: " + a.Name}
			}
			args[a.Name] = value
		}
		return map[string]any{
			"description": p.Description,
			"messages": []map[string]any{{
				"role":    "user",
				"content": map[string]any{"type": "text", "text": p.text(args)},
			}},
		}, nil
	}
	return nil, &rpcError{codeInvalidParams, "unknown prompt: " + in.Name}
}
