package cli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

func retentionDB(t *testing.T) (*sql.DB, paths.Paths) {
	t.Helper()
	withFakeHome(t)
	db, p, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, p
}

func TestRunRetentionDeletesOnlyOldData(t *testing.T) {
	db, p := retentionDB(t)
	now := time.Now()
	seedPurgeRow(t, db, "old", now.AddDate(0, -7, 0).UnixMilli())
	seedPurgeRow(t, db, "new", now.AddDate(0, -5, 0).UnixMilli())

	if err := runRetention(db, p, 6, now); err != nil {
		t.Fatalf("runRetention: %v", err)
	}

	for _, table := range allPurgeableTables {
		if got := countRows(t, db, table); got != 1 {
			t.Errorf("%s: %d rows, want 1", table, got)
		}
	}
	var session string
	if err := db.QueryRow(`SELECT session_id FROM sessions`).Scan(&session); err != nil {
		t.Fatal(err)
	}
	if session != "new" {
		t.Errorf("surviving session = %q, want new", session)
	}
}

func TestRunRetentionClearsSearchIndex(t *testing.T) {
	db, p := retentionDB(t)
	now := time.Now()
	seedPurgeRow(t, db, "old", now.AddDate(-1, 0, 0).UnixMilli())
	if _, err := db.Exec(`UPDATE interactions SET prompt_text = 'findme'`); err != nil {
		t.Fatal(err)
	}

	if err := runRetention(db, p, 6, now); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM interactions_fts WHERE interactions_fts MATCH 'findme'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("search index still finds deleted prompts: %d", n)
	}
}

func TestRunRetentionKeepsOldSessionStillInUse(t *testing.T) {
	db, p := retentionDB(t)
	now := time.Now()
	old := now.AddDate(0, -7, 0).UnixMilli()
	seedPurgeRow(t, db, "long", old)
	if _, err := db.Exec(`INSERT INTO interactions (prompt_id, session_id, seq, started_at) VALUES ('recent', 'long', 2, ?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	if err := runRetention(db, p, 6, now); err != nil {
		t.Fatal(err)
	}

	if got := countRows(t, db, "sessions"); got != 1 {
		t.Errorf("sessions = %d, want the active session kept", got)
	}
	if got := countRows(t, db, "interactions"); got != 1 {
		t.Errorf("interactions = %d, want only the recent one", got)
	}
}

func TestRemoveOldBodies(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	cutoff := now.AddDate(0, -6, 0)
	write := func(rel string, mod time.Time) string {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldFile := write("a/old.json", now.AddDate(-1, 0, 0))
	newFile := write("a/new.json", now)
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.AddDate(-1, 0, 0)
	if err := os.Chtimes(outside, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}

	removed, err := removeOldBodies(dir, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file still there (err=%v)", err)
	}
	for _, path := range []string{newFile, outside} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", path, err)
		}
	}
}

func TestRemoveOldBodiesNoDirectory(t *testing.T) {
	removed, err := removeOldBodies(filepath.Join(t.TempDir(), "none"), time.Now())
	if err != nil || removed != 0 {
		t.Errorf("removeOldBodies = %d, %v, want 0, nil", removed, err)
	}
}

func TestVacuumIfWorthwhile(t *testing.T) {
	db, p := retentionDB(t)
	big := strings.Repeat("x", 4096)
	for i := 0; i < 500; i++ {
		if _, err := db.Exec(`INSERT INTO events_raw (ts, session_id, event_name, body) VALUES (1, 's', 'x', ?)`, big); err != nil {
			t.Fatal(err)
		}
	}

	if vacuumed, err := vacuumIfWorthwhile(db); err != nil || vacuumed {
		t.Fatalf("vacuumed a full database: %v, %v", vacuumed, err)
	}
	if _, err := db.Exec(`DELETE FROM events_raw`); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if vacuumed, err := vacuumIfWorthwhile(db); err != nil || !vacuumed {
		t.Fatalf("didn't vacuum a mostly empty database: %v, %v", vacuumed, err)
	}
	after, err := os.Stat(p.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() >= before.Size() {
		t.Errorf("file went from %d to %d bytes, want it smaller", before.Size(), after.Size())
	}
}

func TestRetentionDue(t *testing.T) {
	db, p := retentionDB(t)
	now := time.Now()

	if !retentionDue(db, now) {
		t.Error("not due on a database that never ran it")
	}
	if err := runRetention(db, p, 6, now); err != nil {
		t.Fatal(err)
	}
	if retentionDue(db, now.Add(23*time.Hour)) {
		t.Error("due again within a day")
	}
	if !retentionDue(db, now.Add(25*time.Hour)) {
		t.Error("not due after a day")
	}
}

func TestRunRetentionLoopOff(t *testing.T) {
	db, p := retentionDB(t)
	seedPurgeRow(t, db, "old", time.Now().AddDate(-2, 0, 0).UnixMilli())

	// Returns at once rather than waiting for ctx, and deletes nothing.
	runRetentionLoop(context.Background(), db, p, 0)

	if got := countRows(t, db, "sessions"); got != 1 {
		t.Errorf("sessions = %d, want 1 with retention off", got)
	}
}

func TestRunRetentionLoopRunsAtStartup(t *testing.T) {
	db, p := retentionDB(t)
	seedPurgeRow(t, db, "old", time.Now().AddDate(-2, 0, 0).UnixMilli())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRetentionLoop(ctx, db, p, 6)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for countRows(t, db, "sessions") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("old data not deleted at startup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestRetentionStatus(t *testing.T) {
	db, p := retentionDB(t)
	if got := retentionStatus(db, 0); !strings.Contains(got, "off") {
		t.Errorf("off: %q", got)
	}
	if got := retentionStatus(db, 6); !strings.Contains(got, "no cleanup yet") {
		t.Errorf("before a run: %q", got)
	}
	if err := runRetention(db, p, 6, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := retentionStatus(db, 6); !strings.Contains(got, "removed 0 items") {
		t.Errorf("after a run: %q", got)
	}
}

func TestConfigRetentionDefaults(t *testing.T) {
	withFakeHome(t)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureBaseDir(); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]int{
		`{"otlp_port":4318}`:                       config.DefaultRetentionMonths,
		`{"otlp_port":4318,"retention_months":0}`:  0,
		`{"otlp_port":4318,"retention_months":12}`: 12,
	} {
		if err := os.WriteFile(p.ConfigPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RetentionMonths != want {
			t.Errorf("%s: RetentionMonths = %d, want %d", body, cfg.RetentionMonths, want)
		}
	}
}
