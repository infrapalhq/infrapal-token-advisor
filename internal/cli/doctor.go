package cli

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/all"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runDoctor(args []string) error {
	fs := newFlagSet("doctor")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, p, err := openDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ok := func(pass bool, label string, fix string) {
		status := "OK"
		if !pass {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s\n", status, label)
		if !pass && fix != "" {
			fmt.Printf("       fix: %s\n", fix)
		}
	}

	// 1. Is the collector listening on the configured port?
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.OTLPPort)
	conn, dialErr := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if dialErr == nil {
		conn.Close()
	}
	ok(dialErr == nil, fmt.Sprintf("collector listening on %s", addr),
		"run `token-advisor start` (foreground) or `token-advisor init` (background service) to bring the collector up")

	// Is the dashboard up? The service runs it alongside the collector.
	ok(dashboardRunning(cfg.APIPort), fmt.Sprintf("dashboard answering on 127.0.0.1:%d", cfg.APIPort),
		"run `token-advisor logs --errors` to see why it didn't start, or run `token-advisor ui`")

	// Will the service come back after a restart?
	if sp, err := servicePaths(p); err == nil {
		if svc, err := newService(sp); err == nil {
			autostartErr := svc.CheckAutostart()
			ok(autostartErr == nil, "background service will restart after a reboot",
				fmt.Sprintf("%v — run `token-advisor init` from a built binary", autostartErr))
		}
	}
	if runtime.GOOS == "darwin" {
		fmt.Println("[INFO] if it still doesn't start at login, check that token-advisor is allowed under System Settings > General > Login Items & Extensions")
	}

	// On Linux, will the service run outside a login session?
	if runtime.GOOS == "linux" {
		if linger, err := service.LingerEnabled(); err == nil && !linger {
			fmt.Println("[INFO] the service runs only while you're logged in — to keep it running after logout, run `loginctl enable-linger` (init tries this, but it can be blocked by system policy)")
		}
	}

	// Has anything gone wrong lately?
	logSummary, err := logging.Summarize(p.LogPath, time.Now().Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	ok(logSummary.Errors == 0, fmt.Sprintf("%d error(s) logged in the last 24 hours", logSummary.Errors),
		"run `token-advisor logs --errors` for details")
	ok(logSummary.Panics == 0, fmt.Sprintf("%d crash(es) in the last 24 hours", logSummary.Panics),
		"run `token-advisor logs --errors` for the stack trace, and include it in a bug report")
	if logSummary.Unclean > 0 {
		fmt.Printf("[INFO] %d unclean shutdown(s) in the last 24 hours — a crash, a kill or a power loss\n", logSummary.Unclean)
	}
	errLog := filepath.Join(p.BaseDir, "collector.err.log")
	if info, statErr := os.Stat(errLog); statErr == nil && info.Size() > 0 && time.Since(info.ModTime()) < 24*time.Hour {
		ok(false, "no fatal output from the service in the last 24 hours",
			fmt.Sprintf("see %s — it holds startup failures and Go runtime crashes", errLog))
	}

	// 2. Is the agent set up to report here?
	checks, err := all.Default(p.Home).Check(cfg.OTLPPort)
	if err != nil {
		return err
	}
	for _, c := range checks {
		if c.Skipped {
			fmt.Printf("[SKIP] %s\n", c.Label)
			continue
		}
		ok(c.Pass, c.Label, c.Fix)
	}

	// 5. Last received metric/event/span timestamps.
	lastEvent, err := maxTS(db, `SELECT MAX(ts) FROM events_raw`)
	if err != nil {
		return err
	}
	lastMetric, err := maxTS(db, `SELECT MAX(ts) FROM metric_points`)
	if err != nil {
		return err
	}
	lastSpanNS, err := maxTS(db, `SELECT MAX(start_ns) FROM spans`)
	if err != nil {
		return err
	}
	fmt.Println("[INFO] last received:")
	fmt.Printf("       events:  %s\n", formatLastTS(lastEvent, time.UnixMilli))
	fmt.Printf("       metrics: %s\n", formatLastTS(lastMetric, time.UnixMilli))
	fmt.Printf("       spans:   %s\n", formatLastTS(lastSpanNS, func(ns int64) time.Time { return time.Unix(0, ns) }))
	if lastEvent.Valid && !lastSpanNS.Valid {
		fmt.Println("       hint: events are arriving but no spans yet — traces beta flag likely isn't set (CLAUDE_CODE_ENHANCED_TELEMETRY_BETA / OTEL_TRACES_EXPORTER).")
	}

	// 6. DB size, WAL size, retention, ingest errors in the last hour.
	if info, statErr := os.Stat(p.DBPath); statErr == nil {
		fmt.Printf("[INFO] db size:  %s\n", humanBytes(info.Size()))
	}
	if info, statErr := os.Stat(p.DBPath + "-wal"); statErr == nil {
		fmt.Printf("[INFO] wal size: %s\n", humanBytes(info.Size()))
	}
	fmt.Printf("[INFO] retention: %s\n", retentionStatus(db, cfg.RetentionMonths))
	since := time.Now().Add(-time.Hour).UnixMilli()
	var errCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM ingest_errors WHERE ts > ?`, since).Scan(&errCount); err != nil {
		return err
	}
	ok(errCount == 0, fmt.Sprintf("%d ingest error(s) in the last hour", errCount),
		"check `SELECT * FROM ingest_errors ORDER BY ts DESC LIMIT 20` in "+p.DBPath+" for details")

	return nil
}

// maxTS runs a MAX(column) query expected to return a single nullable int64.
func maxTS(db *sql.DB, query string) (sql.NullInt64, error) {
	var v sql.NullInt64
	if err := db.QueryRow(query).Scan(&v); err != nil {
		return sql.NullInt64{}, err
	}
	return v, nil
}

// formatLastTS renders a nullable timestamp using toTime to convert the raw
// integer (unix millis for events/metrics, unix nanos for spans) to a
// time.Time, or "none yet" if no rows exist.
func formatLastTS(v sql.NullInt64, toTime func(int64) time.Time) string {
	if !v.Valid {
		return "none yet"
	}
	return toTime(v.Int64).Local().Format(time.RFC3339)
}
