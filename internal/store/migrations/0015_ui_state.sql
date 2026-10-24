-- 0015_ui_state: small key/value store for dashboard state that must be
-- consistent across browsers: last_visit_at (drives "NEW since your last
-- visit") and install_at (baseline for "since install" comparisons).
CREATE TABLE ui_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- install_at defaults to the earliest session already captured.
INSERT OR IGNORE INTO ui_state (key, value)
SELECT 'install_at', CAST(MIN(started_at) AS TEXT) FROM sessions WHERE started_at IS NOT NULL
HAVING MIN(started_at) IS NOT NULL;
