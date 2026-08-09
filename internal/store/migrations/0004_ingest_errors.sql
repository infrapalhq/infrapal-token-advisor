-- 0004_ingest_errors: records batch-write failures so `ccotel doctor`
-- (a fresh, short-lived process with no memory of past server-log lines)
-- can report "N ingest errors in the last hour" per its check #6, rather
-- than that signal only ever existing in stderr.

CREATE TABLE ingest_errors (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  signal TEXT NOT NULL,   -- "logs" | "metrics" | "spans"
  message TEXT NOT NULL
);

CREATE INDEX idx_ingest_errors_ts ON ingest_errors(ts);
