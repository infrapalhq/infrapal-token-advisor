package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// seedExportDB opens the DB under the current (faked) $HOME, inserts one
// interaction with a response, an api_request, and a tool_call carrying a
// full_command, then closes it so runExport/runPurge can reopen it cleanly
// through their normal openDB() path.
func seedExportDB(t *testing.T) {
	t.Helper()
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`INSERT INTO sessions (session_id, started_at) VALUES ('s1', 1000)`,
		`INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
			VALUES ('p1', 's1', 1, 1000, 200, 'delete the prod database')`,
		`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, model, response_text)
			VALUES ('m1', 'p1', 's1', 1100, 'claude-3-opus', 'absolutely not')`,
		`INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, input_tokens, output_tokens, cost_usd_micros)
			VALUES ('req-1', 'p1', 's1', 1000, 'claude-3-opus', 100, 50, 250000)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, full_command)
			VALUES ('tu-1', 'p1', 's1', 1050, 'Bash', 'reject', 1, 'rm -rf /prod')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed exec %q: %v", stmt, err)
		}
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever it wrote. runExport writes straight to os.Stdout with no
// injectable io.Writer, so this is the only way to assert on its output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy captured stdout: %v", err)
	}
	return buf.String()
}

func TestRunExport_JSONL_RedactedByDefault(t *testing.T) {
	withFakeHome(t)
	seedExportDB(t)

	out := captureStdout(t, func() {
		if err := runExport(nil); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 jsonl line, got %d: %q", len(lines), out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["prompt_id"] != "p1" {
		t.Errorf("prompt_id = %v, want p1", rec["prompt_id"])
	}
	if _, present := rec["prompt_text"]; present {
		t.Errorf("prompt_text present without --include-content: %v", rec["prompt_text"])
	}
	if _, present := rec["response_text"]; present {
		t.Errorf("response_text present without --include-content: %v", rec["response_text"])
	}
	if _, present := rec["full_commands"]; present {
		t.Errorf("full_commands present without --include-content: %v", rec["full_commands"])
	}
	if rec["tool_call_count"] != float64(1) {
		t.Errorf("tool_call_count = %v, want 1", rec["tool_call_count"])
	}
	if rec["rejected_count"] != float64(1) {
		t.Errorf("rejected_count = %v, want 1", rec["rejected_count"])
	}
}

func TestRunExport_JSONL_IncludeContent(t *testing.T) {
	withFakeHome(t)
	seedExportDB(t)

	out := captureStdout(t, func() {
		if err := runExport([]string{"--include-content"}); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["prompt_text"] != "delete the prod database" {
		t.Errorf("prompt_text = %v, want the seeded prompt", rec["prompt_text"])
	}
	if rec["response_text"] != "absolutely not" {
		t.Errorf("response_text = %v, want the seeded response", rec["response_text"])
	}
	commands, ok := rec["full_commands"].([]any)
	if !ok || len(commands) != 1 || commands[0] != "rm -rf /prod" {
		t.Errorf("full_commands = %v, want [\"rm -rf /prod\"]", rec["full_commands"])
	}
}

func TestRunExport_CSV(t *testing.T) {
	withFakeHome(t)
	seedExportDB(t)

	out := captureStdout(t, func() {
		if err := runExport([]string{"--format", "csv", "--include-content"}); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})

	cr := csv.NewReader(strings.NewReader(out))
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected header + 1 data row, got %d rows: %v", len(rows), rows)
	}
	header := rows[0]
	data := rows[1]
	col := func(name string) string {
		for i, h := range header {
			if h == name {
				return data[i]
			}
		}
		t.Fatalf("column %q not found in header %v", name, header)
		return ""
	}
	if col("prompt_id") != "p1" {
		t.Errorf("prompt_id = %q, want p1", col("prompt_id"))
	}
	if col("prompt_text") != "delete the prod database" {
		t.Errorf("prompt_text = %q, want the seeded prompt", col("prompt_text"))
	}
}

func TestRunExport_Since(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	// One interaction far in the past (excluded by --since), one "now".
	if _, err := db.Exec(`INSERT INTO sessions (session_id, started_at) VALUES ('s1', 1000)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO interactions (prompt_id, session_id, seq, started_at) VALUES ('old', 's1', 1, 1000)`); err != nil {
		t.Fatalf("seed old interaction: %v", err)
	}
	var recentMs int64
	if err := db.QueryRow(`SELECT CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)`).Scan(&recentMs); err != nil {
		t.Fatalf("compute now: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO interactions (prompt_id, session_id, seq, started_at) VALUES ('recent', 's1', 2, ?)`, recentMs); err != nil {
		t.Fatalf("seed recent interaction: %v", err)
	}
	db.Close()

	out := captureStdout(t, func() {
		if err := runExport([]string{"--since", "1d"}); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line within --since 1d, got %d: %q", len(lines), out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec["prompt_id"] != "recent" {
		t.Errorf("prompt_id = %v, want recent (old row should be excluded by --since)", rec["prompt_id"])
	}
}

func TestRunExport_InvalidFormat(t *testing.T) {
	withFakeHome(t)
	seedExportDB(t)

	if err := runExport([]string{"--format", "xml"}); err == nil {
		t.Error("expected error for --format xml, got nil")
	}
}

func TestRunExport_NoRows(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	db.Close()

	out := captureStdout(t, func() {
		if err := runExport(nil); err != nil {
			t.Fatalf("runExport: %v", err)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output on empty DB, got %q", out)
	}
}
