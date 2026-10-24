package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// timelineRow is one line of a session's thread (SPEC §4.3). Kind is div |
// you | claude | tool | event. Flag/FlagLabel are set when an occurrence
// points at this row, so the UI can show a "!" and jump to its issue.
type timelineRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Ts        int64  `json:"ts"`
	Text      string `json:"text"`
	Name      string `json:"name,omitempty"`
	Meta      string `json:"meta,omitempty"`
	PromptID  string `json:"prompt_id,omitempty"`
	Context   *int64 `json:"context_tokens,omitempty"`
	WaitMs    int64  `json:"wait_ms,omitempty"`
	Flag      string `json:"flag,omitempty"`
	FlagLabel string `json:"flag_label,omitempty"`
}

// handleSessionTimeline handles GET /api/sessions/{id}/timeline: the session's
// thread in order, each step's context size, and compaction markers.
func (s *Server) handleSessionTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, occ, err := computeInsights(r.Context(), s.db, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sess, ok := d.sessions[id]
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	rows, err := buildTimeline(r.Context(), s.db, d, occ, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var peak int64
	compactions := 0
	for _, row := range rows {
		if row.Context != nil && *row.Context > peak {
			peak = *row.Context
		}
		if row.Kind == "event" {
			compactions++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":   id,
		"started_at":   sess.StartedAt,
		"rows":         rows,
		"peak_context": peak,
		"compactions":  compactions,
		"receipt":      buildReceipt(d, occ, id),
	})
}

// buildTimeline merges prompts, Claude's replies, tool calls and compactions
// for one session into ordered rows, and attaches context sizes.
func buildTimeline(ctx context.Context, db *sql.DB, d *insightData, occ []occurrence, sid string) ([]timelineRow, error) {
	flags := map[string]occurrence{}
	for _, o := range occ {
		if o.SessionID != sid {
			continue
		}
		for _, ev := range o.Evidence {
			if _, ok := flags[ev]; !ok {
				flags[ev] = o
			}
		}
	}

	// requests of this session, already ordered by ts
	var reqs []reqRow
	for _, r := range d.reqs {
		if r.SessionID == sid {
			reqs = append(reqs, r)
		}
	}
	// context a row carries: latest request at or before it; a tool row takes
	// the first request after it, since that request is the one that read its
	// result.
	ctxAt := func(ts int64, after bool) *int64 {
		if len(reqs) == 0 {
			return nil
		}
		if after {
			i := sort.Search(len(reqs), func(i int) bool { return reqs[i].Ts >= ts })
			if i < len(reqs) {
				v := reqs[i].Context
				return &v
			}
			return nil
		}
		i := sort.Search(len(reqs), func(i int) bool { return reqs[i].Ts > ts })
		if i > 0 {
			v := reqs[i-1].Context
			return &v
		}
		return nil
	}

	var rows []timelineRow
	promptCost := map[string]float64{}
	for _, r := range reqs {
		promptCost[r.PromptID] += r.CostUSD
	}
	n := 0
	for _, p := range d.prompts {
		if p.SessionID != sid {
			continue
		}
		n++
		rows = append(rows, timelineRow{
			ID: "d:" + p.PromptID, Kind: "div", Ts: p.StartedAt,
			Text: fmt.Sprintf("PROMPT %d", n), PromptID: p.PromptID,
			Meta: fmt.Sprintf("%s · $%.2f", time.UnixMilli(p.StartedAt).Format("15:04"), promptCost[p.PromptID]),
		})
		rows = append(rows, timelineRow{
			ID: "u:" + p.PromptID, Kind: "you", Ts: p.StartedAt, Text: p.Text, PromptID: p.PromptID,
			Context: ctxAt(p.StartedAt, false),
		})
	}

	claude, err := loadClaudeReplies(ctx, db, sid)
	if err != nil {
		return nil, err
	}
	for _, c := range claude {
		rows = append(rows, timelineRow{
			ID: "c:" + c.uuid, Kind: "claude", Ts: c.ts, Text: c.text, PromptID: c.promptID,
			Context: ctxAt(c.ts, false),
		})
	}

	for _, t := range d.tools {
		if t.SessionID != sid {
			continue
		}
		row := timelineRow{
			ID: "t:" + t.ID, Kind: "tool", Ts: t.Ts, Name: t.Name,
			Text: toolLabel(t), PromptID: t.PromptID, Context: ctxAt(t.Ts, true),
		}
		if t.ResultBytes > 0 {
			row.Meta = "≈" + humanTokens(t.ResultBytes/4) + " tok"
		}
		if w, ok := d.waits[t.ID]; ok {
			row.WaitMs = w.WaitMs
		}
		rows = append(rows, row)
	}

	for _, c := range d.compactions {
		if c.SessionID != sid {
			continue
		}
		rows = append(rows, timelineRow{
			ID: fmt.Sprintf("e:%d", c.ID), Kind: "event", Ts: c.Ts,
			Text:    fmt.Sprintf("Context compacted · ~%s before", humanTokens(c.Pre)),
			Context: ctxAt(c.Ts, false),
		})
	}

	kindOrder := map[string]int{"div": 0, "you": 1, "claude": 2, "tool": 3, "event": 4}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Ts != rows[j].Ts {
			return rows[i].Ts < rows[j].Ts
		}
		return kindOrder[rows[i].Kind] < kindOrder[rows[j].Kind]
	})
	for i := range rows {
		if o, ok := flags[rows[i].ID]; ok {
			rows[i].Flag = o.Area
			rows[i].FlagLabel = o.Title
		}
	}
	return rows, nil
}

// claudeReply is one main-thread assistant message.
type claudeReply struct {
	uuid, promptID, text string
	ts                   int64
}

// loadClaudeReplies returns the session's main-thread assistant messages.
func loadClaudeReplies(ctx context.Context, db *sql.DB, sid string) ([]claudeReply, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT message_uuid, COALESCE(prompt_id,''), COALESCE(ts,0), COALESCE(response_text,'')
		FROM assistant_responses
		WHERE session_id = ? AND query_source = 'repl_main_thread'
		ORDER BY ts`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claudeReply
	for rows.Next() {
		var c claudeReply
		if err := rows.Scan(&c.uuid, &c.promptID, &c.ts, &c.text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// toolLabel is the short text a tool row shows: the file for file tools, the
// command for Bash, server · tool for MCP, else the tool name.
func toolLabel(t toolRow) string {
	switch {
	case t.File != "":
		return filepath.Base(t.File)
	case t.Name == "Bash" && t.Command != "":
		return truncate(normalizeCommand(t.Command), 60)
	case t.MCPServer != "":
		return t.MCPServer
	}
	return t.Name
}

// humanTokens renders a token count as 512, 31K, or 1.2M.
func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// buildReceipt totals one session the way the Receipt panel shows it: cost,
// how much of it was avoidable (estimated from the session's occurrences), the
// token mix, and each prompt's cost.
func buildReceipt(d *insightData, occ []occurrence, sid string) map[string]any {
	var cost, avoidable float64
	var input, output, cacheRead, cacheWrite int64
	perPrompt := map[string]float64{}
	perPromptTokens := map[string]int64{}
	calls := 0
	for _, r := range d.reqs {
		if r.SessionID != sid {
			continue
		}
		calls++
		cost += r.CostUSD
		input += r.Input
		output += r.Output
		cacheRead += r.CacheRead
		cacheWrite += r.CacheCreate
		perPrompt[r.PromptID] += r.CostUSD
		perPromptTokens[r.PromptID] += r.Tokens()
	}
	flagged := map[string]int{}
	byArea := map[string]float64{}
	for _, o := range occ {
		if o.SessionID != sid {
			continue
		}
		if o.PromptID != "" {
			flagged[o.PromptID]++
		}
		var usd float64
		switch {
		case o.USD != nil:
			usd = *o.USD
		case o.Tokens != nil && d.blendedRate > 0:
			usd = float64(*o.Tokens) * d.blendedRate
		}
		avoidable += usd
		byArea[o.Area] += usd
	}
	well := 1.0
	if cost > 0 {
		well = 1 - avoidable/cost
		if well < 0 {
			well = 0
		}
	}
	var prompts []map[string]any
	for _, p := range d.prompts {
		if p.SessionID != sid {
			continue
		}
		prompts = append(prompts, map[string]any{
			"prompt_id": p.PromptID,
			"label":     truncate(strings.TrimSpace(p.Text), 40),
			"cost_usd":  perPrompt[p.PromptID],
			"tokens":    perPromptTokens[p.PromptID],
			"flags":     flagged[p.PromptID],
		})
	}
	return map[string]any{
		"cost_usd":      cost,
		"avoidable_usd": avoidable,
		"well_spent":    well,
		"calls":         calls,
		"tokens":        map[string]int64{"fresh_input": input, "output": output, "cache_read": cacheRead, "cache_write": cacheWrite},
		"by_area_usd":   byArea,
		"prompts":       prompts,
	}
}
