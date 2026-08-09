package cli

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// exportRecord is one interactions row, enriched with its assistant
// response and aggregated api_requests/tool_calls totals — the natural
// export unit: one prompt/response turn plus what it cost.
type exportRecord struct {
	PromptID            string   `json:"prompt_id"`
	SessionID           string   `json:"session_id"`
	StartedAt           int64    `json:"started_at"`
	DurationMs          *int64   `json:"duration_ms,omitempty"`
	PromptText          string   `json:"prompt_text,omitempty"`
	ResponseText        string   `json:"response_text,omitempty"`
	Model               string   `json:"model,omitempty"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CostUSD             float64  `json:"cost_usd"`
	ToolCallCount       int64    `json:"tool_call_count"`
	RejectedCount       int64    `json:"rejected_count"`
	FullCommands        []string `json:"full_commands,omitempty"`
}

func runExport(args []string) error {
	fs := newFlagSet("export")
	format := fs.String("format", "jsonl", "output format: jsonl|csv")
	since := fs.String("since", "", "only export data newer than this (e.g. 7d)")
	includeContent := fs.Bool("include-content", false, "include prompt/response/command content (redacted by default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "jsonl" && *format != "csv" {
		return fmt.Errorf("--format must be jsonl or csv, got %q", *format)
	}

	var sinceCutoff int64
	if *since != "" {
		d, err := parseSince(*since)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		sinceCutoff = time.Now().Add(-d).UnixMilli()
	}

	db, _, err := openDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	records, err := loadExportRecords(db, sinceCutoff)
	if err != nil {
		return err
	}

	if !*includeContent {
		for i := range records {
			records[i].PromptText = ""
			records[i].ResponseText = ""
			records[i].FullCommands = nil
		}
	}

	if *format == "jsonl" {
		return writeExportJSONL(os.Stdout, records)
	}
	return writeExportCSV(os.Stdout, records)
}

func loadExportRecords(db *sql.DB, sinceCutoff int64) ([]exportRecord, error) {
	rows, err := db.Query(`
		SELECT
			i.prompt_id, i.session_id, i.started_at, i.duration_ms, COALESCE(i.prompt_text, ''),
			COALESCE(ar_resp.response_text, ''), COALESCE(ar_agg.model, ''),
			COALESCE(ar_agg.input_tokens, 0), COALESCE(ar_agg.output_tokens, 0),
			COALESCE(ar_agg.cache_read_tokens, 0), COALESCE(ar_agg.cache_creation_tokens, 0),
			COALESCE(ar_agg.cost_usd, 0.0),
			COALESCE(tc_agg.tool_call_count, 0), COALESCE(tc_agg.rejected_count, 0)
		FROM interactions i
		LEFT JOIN assistant_responses ar_resp ON ar_resp.prompt_id = i.prompt_id
		LEFT JOIN (
			SELECT prompt_id, MAX(model) AS model,
				SUM(COALESCE(input_tokens, 0)) AS input_tokens,
				SUM(COALESCE(output_tokens, 0)) AS output_tokens,
				SUM(COALESCE(cache_read_tokens, 0)) AS cache_read_tokens,
				SUM(COALESCE(cache_creation_tokens, 0)) AS cache_creation_tokens,
				SUM(COALESCE(cost_usd_micros, 0)) / 1000000.0 AS cost_usd
			FROM api_requests GROUP BY prompt_id
		) ar_agg ON ar_agg.prompt_id = i.prompt_id
		LEFT JOIN (
			SELECT prompt_id, COUNT(*) AS tool_call_count,
				SUM(CASE WHEN decision = 'reject' THEN 1 ELSE 0 END) AS rejected_count
			FROM tool_calls GROUP BY prompt_id
		) tc_agg ON tc_agg.prompt_id = i.prompt_id
		WHERE i.started_at >= ?
		ORDER BY i.started_at
	`, sinceCutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []exportRecord
	for rows.Next() {
		var rec exportRecord
		var durationMs sql.NullInt64
		if err := rows.Scan(
			&rec.PromptID, &rec.SessionID, &rec.StartedAt, &durationMs, &rec.PromptText,
			&rec.ResponseText, &rec.Model,
			&rec.InputTokens, &rec.OutputTokens, &rec.CacheReadTokens, &rec.CacheCreationTokens,
			&rec.CostUSD, &rec.ToolCallCount, &rec.RejectedCount,
		); err != nil {
			return nil, err
		}
		if durationMs.Valid {
			d := durationMs.Int64
			rec.DurationMs = &d
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	commands, err := loadFullCommandsByPrompt(db)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i].FullCommands = commands[records[i].PromptID]
	}

	return records, nil
}

// loadFullCommandsByPrompt fetches every recorded Bash full_command grouped
// by prompt_id, separately from the main aggregate query since a prompt can
// have any number of them (a JOIN would fan out the other aggregates).
func loadFullCommandsByPrompt(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query(`
		SELECT prompt_id, full_command FROM tool_calls
		WHERE prompt_id IS NOT NULL AND full_command IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]string)
	for rows.Next() {
		var promptID, cmd string
		if err := rows.Scan(&promptID, &cmd); err != nil {
			return nil, err
		}
		out[promptID] = append(out[promptID], cmd)
	}
	return out, rows.Err()
}

func writeExportJSONL(w io.Writer, records []exportRecord) error {
	enc := json.NewEncoder(w)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func writeExportCSV(w io.Writer, records []exportRecord) error {
	cw := csv.NewWriter(w)
	header := []string{
		"prompt_id", "session_id", "started_at", "duration_ms", "prompt_text", "response_text",
		"model", "input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens",
		"cost_usd", "tool_call_count", "rejected_count", "full_commands",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range records {
		durationMs := ""
		if r.DurationMs != nil {
			durationMs = fmt.Sprintf("%d", *r.DurationMs)
		}
		fullCommandsJSON := ""
		if len(r.FullCommands) > 0 {
			b, err := json.Marshal(r.FullCommands)
			if err != nil {
				return err
			}
			fullCommandsJSON = string(b)
		}
		row := []string{
			r.PromptID, r.SessionID, fmt.Sprintf("%d", r.StartedAt), durationMs, r.PromptText, r.ResponseText,
			r.Model, fmt.Sprintf("%d", r.InputTokens), fmt.Sprintf("%d", r.OutputTokens),
			fmt.Sprintf("%d", r.CacheReadTokens), fmt.Sprintf("%d", r.CacheCreationTokens),
			fmt.Sprintf("%.6f", r.CostUSD), fmt.Sprintf("%d", r.ToolCallCount), fmt.Sprintf("%d", r.RejectedCount),
			fullCommandsJSON,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
