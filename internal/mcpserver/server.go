// Package mcpserver exposes the captured telemetry to Claude over MCP (JSON-RPC
// on stdio), so Claude can derive its own insights from it. Tool results leave
// the machine with the conversation, so the tools return metrics and ids only:
// run_sql is limited to v_* views without content columns (guard.go), and
// get_findings removes file paths and commands (api.SafeFindings).
package mcpserver

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
)

// protocolVersions lists the MCP revisions this server can speak, newest first.
// The subset used here (initialize, ping, tools) is the same in all of them.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const instructions = "Token Advisor holds this user's Claude Code usage: spend, tokens, cache, tools, permissions, and sessions. " +
	"Start with get_findings for known problems and get_schema for what can be queried, then use run_sql to quantify and compare. " +
	"Drill into one prompt or session through v_api_request_detail and v_tool_call_detail. " +
	"Prompt text, responses, file paths, and commands are not available here."

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Server handles MCP requests over a read-only database.
type Server struct {
	db           *sql.DB
	version      string
	queryTimeout time.Duration
}

// New builds a Server. db should come from store.OpenReadOnly.
func New(db *sql.DB, version string) *Server {
	return &Server{db: db, version: version, queryTimeout: 10 * time.Second}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve answers newline-delimited JSON-RPC messages from in on out until in
// reaches EOF, handling one at a time.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	encoder := json.NewEncoder(out)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if resp := s.handleLine(ctx, line); resp != nil {
				if err := encoder.Encode(resp); err != nil {
					return fmt.Errorf("write response: %w", err)
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read request: %w", readErr)
		}
	}
}

// handleLine returns the response to one message, or nil if it needs none.
func (s *Server) handleLine(ctx context.Context, line []byte) *response {
	if len(trimSpace(line)) == 0 {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return &response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{codeParseError, "parse error"}}
	}
	if req.ID == nil {
		return nil // a notification, such as notifications/initialized
	}
	result, rpcErr := s.dispatch(ctx, req)
	return &response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	return b
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	case "":
		return nil, &rpcError{codeInvalidRequest, "missing method"}
	}
	return nil, &rpcError{codeMethodNotFound, "unknown method: " + req.Method}
}

// initialize echoes the client's protocol version when it is one this server
// speaks, and otherwise offers the newest, as the spec prescribes.
func (s *Server) initialize(params json.RawMessage) any {
	var in struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &in)
	version := protocolVersions[0]
	if slices.Contains(protocolVersions, in.ProtocolVersion) {
		version = in.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "infrapal-token-advisor", "version": s.version},
		"instructions":    instructions,
	}
}

// callTool runs a tool. A failure inside the tool is an isError result rather
// than a protocol error, so the model can read the message and correct itself.
func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var in struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &rpcError{codeInvalidParams, "invalid params"}
	}
	if len(in.Arguments) == 0 {
		in.Arguments = json.RawMessage("{}")
	}
	for _, t := range tools {
		if t.Name != in.Name {
			continue
		}
		result, err := t.call(s, ctx, in.Arguments)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(string(encoded), false), nil
	}
	return nil, &rpcError{codeInvalidParams, "unknown tool: " + in.Name}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
