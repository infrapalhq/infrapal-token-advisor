package cli

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/claudesettings"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
)

// managedSettingsPath returns the platform's enterprise managed-settings.json
// location, or "" on a platform with no known path (e.g. Windows, whose
// managed settings can also arrive via registry rather than a file — not
// checkable here). Confirmed against https://code.claude.com/docs/en/settings.
func managedSettingsPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		return "/etc/claude-code/managed-settings.json"
	default:
		return ""
	}
}

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
		"run `infrapal-token-advisor start` (foreground) or `infrapal-token-advisor init` (background service) to bring the collector up")

	// 2. Does settings.json contain our env block, endpoint matching our port?
	doc, existed, err := claudesettings.Load(p.ClaudeSettingsPath)
	if err != nil {
		return err
	}
	wantEndpoint := fmt.Sprintf("http://localhost:%d", cfg.OTLPPort)
	settingsOK := false
	var settingsDetail string
	if !existed {
		settingsDetail = fmt.Sprintf("%s does not exist", p.ClaudeSettingsPath)
	} else if env, envErr := doc.EnvBlock(); envErr != nil {
		settingsDetail = envErr.Error()
	} else {
		endpoint, _ := env["OTEL_EXPORTER_OTLP_ENDPOINT"].(string)
		var missing []string
		for _, key := range []string{
			"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_LOGS_EXPORTER", "OTEL_TRACES_EXPORTER",
			"OTEL_METRICS_EXPORTER", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA",
		} {
			if _, present := env[key]; !present {
				missing = append(missing, key)
			}
		}
		if endpoint != wantEndpoint {
			settingsDetail = fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT is %q, want %q", endpoint, wantEndpoint)
		} else if len(missing) > 0 {
			settingsDetail = fmt.Sprintf("missing keys: %s", strings.Join(missing, ", "))
		} else {
			settingsOK = true
		}
	}
	ok(settingsOK, fmt.Sprintf("%s has infrapal-token-advisor's env block", p.ClaudeSettingsPath),
		fmt.Sprintf("%s — run `infrapal-token-advisor init` to fix", settingsDetail))

	// 3. Conflicting OTEL_*/CLAUDE_CODE_* vars in doctor's own shell env.
	var conflicting []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "OTEL_") || strings.HasPrefix(key, "CLAUDE_CODE_") {
			conflicting = append(conflicting, key)
		}
	}
	if len(conflicting) == 0 {
		ok(true, "no OTEL_*/CLAUDE_CODE_* vars exported in this shell", "")
	} else {
		ok(false, "no OTEL_*/CLAUDE_CODE_* vars exported in this shell",
			fmt.Sprintf("this shell exports: %s — these can override settings.json for any `claude` launched from here; unset them or reconcile with `infrapal-token-advisor init`'s values. Note: this only reflects the shell doctor was invoked from, not every shell you might launch `claude` from.", strings.Join(conflicting, ", ")))
	}

	// 4. Managed settings file present (strips developer-set OTLP endpoints)?
	if mp := managedSettingsPath(); mp == "" {
		fmt.Printf("[SKIP] managed settings check — no known path for GOOS=%s\n", runtime.GOOS)
	} else if _, statErr := os.Stat(mp); statErr == nil {
		ok(false, fmt.Sprintf("no managed settings file at %s", mp),
			"a managed-settings.json is present and can strip developer-set OTLP endpoint/protocol at startup — check it with `claude --debug` and coordinate with whoever manages it if telemetry isn't reaching ccotel")
	} else {
		ok(true, fmt.Sprintf("no managed settings file at %s", mp), "")
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

	// 6. DB size, WAL size, ingest errors in the last hour.
	if info, statErr := os.Stat(p.DBPath); statErr == nil {
		fmt.Printf("[INFO] db size:  %s\n", humanBytes(info.Size()))
	}
	if info, statErr := os.Stat(p.DBPath + "-wal"); statErr == nil {
		fmt.Printf("[INFO] wal size: %s\n", humanBytes(info.Size()))
	}
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
