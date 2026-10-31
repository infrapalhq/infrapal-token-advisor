package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/api"
)

// Limits on what one tool call can put into the model's context.
const (
	maxRows     = 500
	maxBytes    = 256 * 1024
	maxCellRune = 1000
	maxFindings = 50
)

// tool is one MCP tool: its listing and its handler.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`

	call func(s *Server, ctx context.Context, args json.RawMessage) (any, error)
}

func stringParam(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

var readOnly = map[string]any{"readOnlyHint": true}

var tools = []tool{
	{
		Name:        "get_schema",
		Description: "List the SQL views run_sql can read, with their columns. Call this before writing SQL.",
		InputSchema: objectSchema(map[string]any{}),
		Annotations: readOnly,
		call:        (*Server).getSchema,
	},
	{
		Name: "run_sql",
		Description: "Run one read-only SELECT or WITH query against the views from get_schema, for ranking, comparing, and trends. " +
			"Results stop at 500 rows or 256 KB, so aggregate and use LIMIT. Times (started_at, ended_at, ts) are Unix milliseconds. " +
			"Costs are USD unless the column says otherwise.",
		InputSchema: objectSchema(map[string]any{"query": stringParam("The SQL query")}, "query"),
		Annotations: readOnly,
		call:        (*Server).runSQL,
	},
	{
		Name: "get_findings",
		Description: "List the problems Token Advisor's rules detected, highest priority first, with estimated impact. " +
			"File paths and commands are withheld; the user can see them on the dashboard's Fixes page.",
		InputSchema: objectSchema(map[string]any{
			"since": stringParam(`Time range: "today", "7d", "30d", "3m", "install", or "all". Defaults to "30d".`),
		}),
		Annotations: readOnly,
		call:        (*Server).getFindings,
	},
}

// viewAccess reads the live schema: which views the model may read, and every
// other table or view name, which it may not. Read on each call so a new view
// is picked up, and so a new table is blocked, without code changes.
func (s *Server) viewAccess(ctx context.Context) (views map[string][]string, blocked map[string]bool, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view')`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	blocked = map[string]bool{}
	var candidates []string
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			return nil, nil, err
		}
		if kind == "view" && strings.HasPrefix(name, "v_") {
			candidates = append(candidates, name)
		} else {
			blocked[strings.ToLower(name)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()

	views = map[string][]string{}
	for _, view := range candidates {
		columns, err := s.viewColumns(ctx, view)
		if err != nil {
			return nil, nil, err
		}
		safe := true
		for _, c := range columns {
			safe = safe && !contentColumns[strings.ToLower(c)]
		}
		if safe {
			views[view] = columns
		} else {
			blocked[strings.ToLower(view)] = true
		}
	}
	return views, blocked, nil
}

func (s *Server) viewColumns(ctx context.Context, view string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, view)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

func (s *Server) getSchema(ctx context.Context, _ json.RawMessage) (any, error) {
	views, _, err := s.viewAccess(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"views": views}, nil
}

func (s *Server) runSQL(ctx context.Context, args json.RawMessage) (any, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	_, blocked, err := s.viewAccess(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkQuery(in.Query, blocked); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, in.Query)
	if err != nil {
		return nil, queryError(ctx, s, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	out := [][]any{}
	size, truncated := 0, false
	for rows.Next() {
		if len(out) == maxRows {
			truncated = true
			break
		}
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, queryError(ctx, s, err)
		}
		for i, v := range values {
			values[i] = jsonCell(v)
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		if size += len(encoded); size > maxBytes {
			truncated = true
			break
		}
		out = append(out, values)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(ctx, s, err)
	}
	return map[string]any{"columns": columns, "rows": out, "row_count": len(out), "truncated": truncated}, nil
}

func queryError(ctx context.Context, s *Server, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("query timed out after %s: narrow it or aggregate less", s.queryTimeout)
	}
	return err
}

// jsonCell makes a scanned value JSON-friendly and caps long text.
func jsonCell(v any) any {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	if text, ok := v.(string); ok {
		if r := []rune(text); len(r) > maxCellRune {
			return string(r[:maxCellRune]) + "…"
		}
	}
	return v
}

func (s *Server) getFindings(ctx context.Context, args json.RawMessage) (any, error) {
	var in struct {
		Since string `json:"since"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Since == "" {
		in.Since = "30d"
	}
	findings, err := api.SafeFindings(ctx, s.db, in.Since)
	if err != nil {
		return nil, err
	}
	total := len(findings)
	return map[string]any{
		"since":    in.Since,
		"total":    total,
		"findings": findings[:min(total, maxFindings)],
	}, nil
}
