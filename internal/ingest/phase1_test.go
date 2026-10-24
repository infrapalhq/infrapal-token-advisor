package ingest

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestCompactionProjectsIntoTypedTable(t *testing.T) {
	db := openTestDB(t)
	ev := mkEvent("claude_code.compaction", "sess-c", "prompt-c", map[string]any{
		"pre_tokens":  int64(223912),
		"post_tokens": int64(24940),
		"trigger":     "auto",
		"duration_ms": int64(66877),
	})
	ev.Ts = 1_700_000_500_000
	if err := writeLogsBatch(db, []RawEvent{ev, ev}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var n int
	var pre, post, dur int64
	var trig string
	if err := db.QueryRow(`SELECT COUNT(*), MAX(pre_tokens), MAX(post_tokens), MAX(trigger), MAX(duration_ms) FROM compactions`).
		Scan(&n, &pre, &post, &trig, &dur); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("redelivered event must not duplicate: got %d rows", n)
	}
	if pre != 223912 || post != 24940 || trig != "auto" || dur != 66877 {
		t.Fatalf("bad projection: pre=%d post=%d trigger=%q dur=%d", pre, post, trig, dur)
	}
}

func TestSessionProjectDirFromFilePaths(t *testing.T) {
	db := openTestDB(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "main.tf")

	claudeProjectsDir = t.TempDir() // no transcript: force the file-path path
	t.Cleanup(func() { claudeProjectsDir = defaultClaudeProjectsDir() })

	decision := mkEvent("claude_code.tool_decision", "sess-p", "prompt-p", map[string]any{
		"tool_use_id": "tu-p", "tool_name": "Read", "decision": "accept", "source": "config",
	})
	if err := writeLogsBatch(db, []RawEvent{decision}); err != nil {
		t.Fatal(err)
	}
	result := mkEvent("claude_code.tool_result", "sess-p", "prompt-p", map[string]any{
		"tool_use_id": "tu-p", "tool_name": "Read", "success": true,
		"tool_input": `{"file_path":"` + file + `"}`,
	})
	if err := writeLogsBatch(db, []RawEvent{result}); err != nil {
		t.Fatal(err)
	}

	var got string
	if err := db.QueryRow(`SELECT COALESCE(project_dir,'') FROM sessions WHERE session_id='sess-p'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Fatalf("project_dir: want %q got %q", repo, got)
	}
}

func TestUserWaitsCapAndExclusions(t *testing.T) {
	db := openTestDB(t)
	stmts := []string{
		// hand decision, 2 min wait -> counted, no idle
		`INSERT INTO tool_calls (tool_use_id, session_id, tool_name, decision_source, blocked_on_user_ms) VALUES ('w1','s','Bash','user_temporary',120000)`,
		// hand decision, 22 h wait -> capped at 10 min, rest is idle
		`INSERT INTO tool_calls (tool_use_id, session_id, tool_name, decision_source, blocked_on_user_ms) VALUES ('w2','s','Bash','user_temporary',79200000)`,
		// automatic decision -> not a wait
		`INSERT INTO tool_calls (tool_use_id, session_id, tool_name, decision_source, blocked_on_user_ms) VALUES ('w3','s','Bash','config',461)`,
		// designed to block -> excluded
		`INSERT INTO tool_calls (tool_use_id, session_id, tool_name, decision_source, blocked_on_user_ms) VALUES ('w4','s','ExitPlanMode','user_temporary',300000)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(`SELECT tool_use_id, wait_ms, idle_ms FROM v_user_waits ORDER BY tool_use_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string][2]int64{}
	for rows.Next() {
		var id string
		var w, i int64
		if err := rows.Scan(&id, &w, &i); err != nil {
			t.Fatal(err)
		}
		got[id] = [2]int64{w, i}
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows (w1,w2), got %v", got)
	}
	if got["w1"] != [2]int64{120000, 0} {
		t.Fatalf("w1: %v", got["w1"])
	}
	if got["w2"] != [2]int64{600000, 79200000 - 600000} {
		t.Fatalf("w2: %v", got["w2"])
	}
}

func TestInstallAtKeepsEarliestSession(t *testing.T) {
	db := openTestDB(t)
	first := mkEvent("claude_code.user_prompt", "sess-a", "p-a", map[string]any{"prompt_length": int64(5)})
	first.Ts = 1_700_000_100_000
	later := mkEvent("claude_code.user_prompt", "sess-b", "p-b", map[string]any{"prompt_length": int64(5)})
	later.Ts = 1_700_000_900_000
	if err := writeLogsBatch(db, []RawEvent{later}); err != nil {
		t.Fatal(err)
	}
	if err := writeLogsBatch(db, []RawEvent{first}); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM ui_state WHERE key='install_at'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "1700000100000" {
		t.Fatalf("install_at is the earliest session ts, got %s", v)
	}
}

func TestBackfillResolvesExistingSessions(t *testing.T) {
	db := openTestDB(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeProjectsDir = t.TempDir()
	t.Cleanup(func() { claudeProjectsDir = defaultClaudeProjectsDir() })

	stmts := []string{
		`INSERT INTO sessions (session_id, started_at) VALUES ('old-1', 1000)`,
		`INSERT INTO tool_calls (tool_use_id, session_id, tool_name, tool_input) VALUES ('bf-1', 'old-1', 'Read', '{"file_path":"` + filepath.Join(repo, "main.tf") + `"}')`,
		`INSERT INTO sessions (session_id, started_at) VALUES ('old-2', 2000)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	n, err := BackfillProjectDirs(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("one session is resolvable from its file paths, got %d", n)
	}
	var dir sql.NullString
	db.QueryRow(`SELECT project_dir FROM sessions WHERE session_id='old-1'`).Scan(&dir)
	if dir.String != repo {
		t.Fatalf("old-1 project_dir: want %q got %q", repo, dir.String)
	}
	db.QueryRow(`SELECT project_dir FROM sessions WHERE session_id='old-2'`).Scan(&dir)
	if dir.Valid {
		t.Fatalf("old-2 has no evidence and must stay NULL, got %q", dir.String)
	}
}
