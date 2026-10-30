package cli

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// purgeTable describes one typed table's shape for purge purposes: its
// timestamp column (empty if it has none) and whether it carries
// session_id, so --before/--session can be applied generically instead of
// hand-writing a DELETE per table. beforeFilter narrows --before further.
type purgeTable struct {
	name         string
	tsColumn     string
	tsIsNanos    bool
	hasSessionID bool
	beforeFilter string
}

var purgeTables = []purgeTable{
	{name: "events_raw", tsColumn: "ts", hasSessionID: true},
	{name: "interactions", tsColumn: "started_at", hasSessionID: true},
	// After interactions, so an old session still in use keeps its row.
	{name: "sessions", tsColumn: "started_at", hasSessionID: true,
		beforeFilter: "session_id NOT IN (SELECT session_id FROM interactions WHERE session_id IS NOT NULL)"},
	{name: "assistant_responses", tsColumn: "ts", hasSessionID: true},
	{name: "api_requests", tsColumn: "ts", hasSessionID: true},
	{name: "tool_calls", tsColumn: "ts", hasSessionID: true},
	{name: "spans", tsColumn: "start_ns", tsIsNanos: true, hasSessionID: true},
	{name: "metric_points", tsColumn: "ts", hasSessionID: true},
	{name: "ingest_errors", tsColumn: "ts", hasSessionID: false},
	{name: "mcp_server_connections", tsColumn: "ts", hasSessionID: true},
	{name: "compactions", tsColumn: "ts", hasSessionID: true},
	{name: "ai_recommendations", tsColumn: "generated_at"},
}

// purgeOnlyOnAllTables are staging tables with no ts/session_id column to
// filter on — they only get cleared by --all.
var purgeOnlyOnAllTables = []string{
	"pending_interaction_spans",
	"pending_blocked_on_user_spans",
}

func runPurge(args []string) error {
	fs := newFlagSet("purge")
	before := fs.String("before", "", "delete data older than this (e.g. 30d)")
	session := fs.String("session", "", "delete data for a single session id")
	all := fs.Bool("all", false, "delete all captured data")
	if err := fs.Parse(args); err != nil {
		return err
	}

	selected := 0
	if *before != "" {
		selected++
	}
	if *session != "" {
		selected++
	}
	if *all {
		selected++
	}
	if selected != 1 {
		return fmt.Errorf("exactly one of --before, --session, or --all is required")
	}

	var cutoffMs int64
	if *before != "" {
		d, err := parseSince(*before)
		if err != nil {
			return fmt.Errorf("--before: %w", err)
		}
		cutoffMs = time.Now().Add(-d).UnixMilli()
	}

	db, p, err := openDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	deleted, err := purgeData(db, *before != "", cutoffMs, *session, *all)
	if err != nil {
		return err
	}

	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	// A backup still holds what was just deleted.
	if removed, err := store.RemoveBackups(p.DBPath, time.Now()); err != nil {
		return fmt.Errorf("remove database backups: %w", err)
	} else if removed > 0 {
		fmt.Printf("removed %d database backup(s), which still held that data\n", removed)
	}

	fmt.Printf("deleted %d rows from %s\n", deleted, p.DBPath)
	return nil
}

// purgeData runs every table's DELETE inside one transaction, so a failure
// partway through leaves the database unchanged. VACUUM (in runPurge, after
// this returns) is deliberately outside the transaction — SQLite doesn't
// allow VACUUM inside one.
func purgeData(db *sql.DB, byBefore bool, cutoffMs int64, sessionID string, all bool) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var total int64
	for _, t := range purgeTables {
		var res sql.Result
		var err error
		switch {
		case all:
			res, err = tx.Exec("DELETE FROM " + t.name)
		case sessionID != "" && t.hasSessionID:
			res, err = tx.Exec("DELETE FROM "+t.name+" WHERE session_id = ?", sessionID)
		case byBefore && t.tsColumn != "":
			cutoff := cutoffMs
			if t.tsIsNanos {
				cutoff = cutoffMs * int64(time.Millisecond/time.Nanosecond)
			}
			where := t.tsColumn + " < ?"
			if t.beforeFilter != "" {
				where += " AND " + t.beforeFilter
			}
			res, err = tx.Exec("DELETE FROM "+t.name+" WHERE "+where, cutoff)
		default:
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("delete from %s: %w", t.name, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}

	if all {
		for _, name := range purgeOnlyOnAllTables {
			res, err := tx.Exec("DELETE FROM " + name)
			if err != nil {
				return 0, fmt.Errorf("delete from %s: %w", name, err)
			}
			n, _ := res.RowsAffected()
			total += n
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}
