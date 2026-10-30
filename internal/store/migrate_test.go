package store

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// openAt builds a database with migrations applied up to version, as an older
// build would have left it.
func openAt(t *testing.T, dbPath string, version int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(createSchemaMigrations); err != nil {
		t.Fatal(err)
	}
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migs {
		if m.version > version {
			break
		}
		if err := applyMigration(db, m); err != nil {
			t.Fatalf("apply %s: %v", m.name, err)
		}
	}
	return db
}

func latestVersion(t *testing.T) int {
	t.Helper()
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	return migs[len(migs)-1].version
}

// schemaOf returns every schema object's definition, ignoring creation order.
func schemaOf(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT type || ' ' || name, COALESCE(sql, '') FROM sqlite_master`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		out[name] = def
	}
	return out
}

func TestMigrationVersionsHaveNoGaps(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range migs {
		if m.version != i+1 {
			t.Errorf("%s has version %d, want %d", m.name, m.version, i+1)
		}
	}
}

// Every version an existing install could be on must reach the same schema
// as a fresh install.
func TestUpgradeFromEveryVersionMatchesFreshInstall(t *testing.T) {
	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	want := schemaOf(t, fresh)

	latest := latestVersion(t)
	for version := 1; version < latest; version++ {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "old.db")
			db := openAt(t, dbPath, version)
			if err := Migrate(db, dbPath); err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			got := schemaOf(t, db)
			for name, def := range want {
				if got[name] != def {
					t.Errorf("%s differs:\n got %q\nwant %q", name, got[name], def)
				}
			}
			for name := range got {
				if _, ok := want[name]; !ok {
					t.Errorf("%s isn't in a fresh install", name)
				}
			}
		})
	}
}

func TestUpgradeKeepsData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	db := openAt(t, dbPath, 1)
	if _, err := db.Exec(`INSERT INTO sessions (session_id, started_at) VALUES ('s1', 1000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO interactions (prompt_id, session_id, prompt_text) VALUES ('p1', 's1', 'center a div')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var promptID string
	if err := db.QueryRow(`SELECT i.prompt_id FROM interactions_fts
		JOIN interactions i ON i.rowid = interactions_fts.rowid
		WHERE interactions_fts MATCH 'center'`).Scan(&promptID); err != nil {
		t.Fatalf("search after upgrade: %v", err)
	}
	if promptID != "p1" {
		t.Errorf("found %q, want p1", promptID)
	}
}

func TestMigrateBacksUpExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	db := openAt(t, dbPath, 5)
	if _, err := db.Exec(`INSERT INTO sessions (session_id) VALUES ('s1')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	backups, err := Backups(dbPath)
	if err != nil || len(backups) != 1 {
		t.Fatalf("Backups = %v, %v, want one", backups, err)
	}
	if !strings.Contains(backups[0], ".backup-v5-") {
		t.Errorf("backup name %q doesn't give its version", backups[0])
	}
	info, err := os.Stat(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %o, want 600", info.Mode().Perm())
	}

	// The backup is a working database at the old version, with the data.
	copy, err := sql.Open("sqlite", backups[0])
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	if v, err := CurrentVersion(copy); err != nil || v != 5 {
		t.Errorf("backup at v%d, %v, want v5", v, err)
	}
	var n int
	if err := copy.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil || n != 1 {
		t.Errorf("backup has %d sessions, %v, want 1", n, err)
	}
}

func TestMigrateSkipsBackupWhenNothingToDo(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "new.db")
	db, err := Open(dbPath) // a fresh install has nothing to protect
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db, dbPath); err != nil { // up to date
		t.Fatal(err)
	}
	if backups, _ := Backups(dbPath); len(backups) != 0 {
		t.Errorf("unexpected backups: %v", backups)
	}
}

func TestMigrateStopsIfBackupFails(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")
	db := openAt(t, dbPath, 5)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := Migrate(db, dbPath); err == nil {
		t.Fatal("Migrate succeeded with no backup")
	}
	if v, err := CurrentVersion(db); err != nil || v != 5 {
		t.Errorf("database moved to v%d, %v, want it left at v5", v, err)
	}
}

func TestMigrateRefusesNewerDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "new.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	future := latestVersion(t) + 1
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name) VALUES (?, 'future.sql')`, future); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = Open(dbPath)
	if !errors.Is(err, ErrNewerDatabase) {
		t.Fatalf("Open = %v, want ErrNewerDatabase", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("v%d", future)) {
		t.Errorf("error doesn't name the version: %v", err)
	}
}

func TestFailedMigrationLeavesNoTrace(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	db := openAt(t, dbPath, 1)
	bad := migration{version: 99, name: "0099_bad.sql", sql: `
		CREATE TABLE half_done (id INTEGER);
		SELECT * FROM no_such_table;`}

	if err := applyMigration(db, bad); err == nil {
		t.Fatal("applyMigration succeeded")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'half_done'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the failed migration's table is still there")
	}
	if v, err := CurrentVersion(db); err != nil || v != 1 {
		t.Errorf("version = %d, %v, want 1", v, err)
	}
	// The connection must be usable again, not stuck in the transaction.
	if _, err := db.Exec(`INSERT INTO sessions (session_id) VALUES ('after')`); err != nil {
		t.Errorf("database unusable after a failed migration: %v", err)
	}
}

func TestApplyMigrationTwiceIsHarmless(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	db := openAt(t, dbPath, 2)
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, migs[1]); err != nil { // another process got there first
		t.Fatalf("re-apply: %v", err)
	}
}

func TestConcurrentOpensUpgradeOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	openAt(t, dbPath, 5).Close()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(dbPath)
			if err == nil {
				db.Close()
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Errorf("Open: %v", err)
		}
	}
}

func TestBackupsArePruned(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	db := openAt(t, dbPath, 5)
	base := time.Now()
	for i := 0; i < backupsKept+2; i++ {
		path, err := backupDatabase(db, dbPath, 5, base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		// Ordered by age, so give each a clearly later time than the last.
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneBackups(dbPath, backupsKept); err != nil {
		t.Fatal(err)
	}
	if backups, err := Backups(dbPath); err != nil || len(backups) != backupsKept {
		t.Errorf("Backups = %v, %v, want %d", backups, err, backupsKept)
	}
}

func TestRemoveBackupsByAge(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "[odd] dir.db")
	db := openAt(t, dbPath, 5)
	now := time.Now()
	old, err := backupDatabase(db, dbPath, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	long := now.AddDate(0, -2, 0)
	if err := os.Chtimes(old, long, long); err != nil {
		t.Fatal(err)
	}
	recent, err := backupDatabase(db, dbPath, 5, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveBackups(dbPath, now.AddDate(0, -1, 0))
	if err != nil || removed != 1 {
		t.Fatalf("RemoveBackups = %d, %v, want 1, nil", removed, err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent backup gone: %v", err)
	}
}

// Released migrations must never change: installs have already run them, so
// an edit would leave them with a different schema from new ones.
func TestReleasedMigrationsUnchanged(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "migrations.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	want := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		sum, name, ok := strings.Cut(scanner.Text(), "  ")
		if !ok {
			t.Fatalf("bad line in migrations.sha256: %q", scanner.Text())
		}
		want[name] = sum
	}

	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range migs {
		seen[m.name] = true
		sum := sha256.Sum256([]byte(m.sql))
		got := hex.EncodeToString(sum[:])
		switch wantSum, ok := want[m.name]; {
		case !ok:
			t.Errorf("%s is new: add this line to testdata/migrations.sha256:\n%s  %s", m.name, got, m.name)
		case got != wantSum:
			t.Errorf("%s changed: add a new migration instead of editing a released one", m.name)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("%s was removed: released migrations must stay", name)
		}
	}
}
