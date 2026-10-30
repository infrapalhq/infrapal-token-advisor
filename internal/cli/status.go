package cli

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

func runStatus(args []string) error {
	fs := newFlagSet("status")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, p, err := openDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	fmt.Printf("service:     %s\n", serviceStatusLine(p))
	fmt.Printf("database:    %s\n", p.DBPath)

	if info, err := os.Stat(p.DBPath); err == nil {
		fmt.Printf("db size:     %s\n", humanBytes(info.Size()))
	}
	if info, err := os.Stat(p.DBPath + "-wal"); err == nil {
		fmt.Printf("wal size:    %s\n", humanBytes(info.Size()))
	}

	version, err := store.CurrentVersion(db)
	if err != nil {
		return err
	}
	fmt.Printf("schema:      v%d\n", version)

	count, err := scanCount(db, `SELECT COUNT(*) FROM events_raw`)
	if err != nil {
		return err
	}
	fmt.Printf("events:      %d\n", count)

	var lastTS sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(ts) FROM events_raw`).Scan(&lastTS); err != nil {
		return err
	}
	if lastTS.Valid {
		fmt.Printf("last event:  %s\n", time.UnixMilli(lastTS.Int64).Local().Format(time.RFC3339))
	} else {
		fmt.Println("last event:  none yet")
	}

	fmt.Printf("log:         %s\n", p.LogPath)
	summary, err := logging.Summarize(p.LogPath, time.Now().Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	fmt.Printf("errors 24h:  %d (%d crashes)\n", summary.Errors, summary.Panics)

	return nil
}

// serviceStatusLine reports the background service's install/run state,
// falling back to a platform-appropriate message rather than failing the
// whole status command.
func serviceStatusLine(p paths.Paths) string {
	sp, err := servicePaths(p)
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	svc, err := newService(sp)
	if err == service.ErrUnsupported {
		return "no supported service manager on this platform"
	}
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	st, err := svc.Status()
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	switch {
	case !st.Installed:
		return "not installed — run `token-advisor init`"
	case st.Running:
		return "running — " + st.Detail
	default:
		return "installed but not running — " + st.Detail
	}
}

func scanCount(db *sql.DB, query string) (int64, error) {
	var n int64
	if err := db.QueryRow(query).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
