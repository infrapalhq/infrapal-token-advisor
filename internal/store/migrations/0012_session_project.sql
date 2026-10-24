-- 0012_session_project: the project directory a session ran in. OTel carries
-- no cwd, so it's resolved from Claude Code's transcript (cwd field) or the
-- session's file paths and persisted here at first sight, because transcripts
-- are deleted after 30 days (retention_sweep). NULL = not yet resolved.
ALTER TABLE sessions ADD COLUMN project_dir TEXT;
