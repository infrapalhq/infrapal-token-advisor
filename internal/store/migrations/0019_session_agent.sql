-- 0019_session_agent: which coding agent a session came from, using the ids in
-- internal/agent. Every session so far is Claude Code's, and so is one written
-- by anything that doesn't say otherwise.
ALTER TABLE sessions ADD COLUMN agent TEXT NOT NULL DEFAULT 'claude-code';
