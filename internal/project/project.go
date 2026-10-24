// Package project works out which project directory a Claude Code session
// ran in. OpenTelemetry carries no working directory, so this is inferred
// from two local sources, in order:
//
//  1. the transcript Claude Code writes per session
//     (~/.claude/projects/<dir>/<session_id>.jsonl), whose records carry a
//     "cwd" field — exact, but deleted after 30 days by Claude Code's own
//     retention sweep, so it must be read while it exists;
//  2. the session's file paths (from tool_calls.file_path): the nearest
//     ancestor containing a .git entry, else the common directory prefix.
//
// The result is persisted on sessions.project_dir by the caller.
package project

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// maxTranscriptLines bounds how far into a transcript we look for a cwd.
// The first records of a session carry it, so a short scan is enough.
const maxTranscriptLines = 64

// Resolve returns the best project directory for sessionID, or "" if nothing
// local can say. claudeProjectsDir is the ~/.claude/projects root (injected
// so tests don't touch the real home directory). filePaths are the session's
// observed file paths; they may be empty.
func Resolve(claudeProjectsDir, sessionID string, filePaths []string) string {
	if dir := transcriptCWD(claudeProjectsDir, sessionID); dir != "" {
		return dir
	}
	return fromFilePaths(filePaths)
}

// transcriptCWD finds <root>/<any>/<sessionID>.jsonl and returns the first
// cwd recorded in it.
func transcriptCWD(root, sessionID string) string {
	if root == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for i := 0; sc.Scan() && i < maxTranscriptLines; i++ {
		var rec struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.CWD != "" {
			return rec.CWD
		}
	}
	return ""
}

// fromFilePaths returns the git repository that holds the session's most
// common file directory, or "" when that directory is in no repository. A
// common parent folder is deliberately not used: files scattered across the
// home directory would yield "/Users/x", which names no project. Paths under
// ~/.claude (plans, transcripts) are Claude Code's own files, not the project's.
func fromFilePaths(filePaths []string) string {
	dirs := map[string]int{}
	for _, p := range filePaths {
		if p == "" || !filepath.IsAbs(p) || strings.Contains(p, string(filepath.Separator)+".claude"+string(filepath.Separator)) {
			continue
		}
		dirs[filepath.Dir(p)]++
	}
	if len(dirs) == 0 {
		return ""
	}

	var top string
	topN := -1
	for d, n := range dirs {
		if n > topN || (n == topN && d < top) {
			top, topN = d, n
		}
	}
	return gitRoot(top)
}

// gitRoot walks up from dir and returns the first directory holding .git, or
// "" if there is none before the filesystem root.
func gitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Name returns the display name for a project directory: its basename, or
// "(unknown)" when the directory could not be resolved.
func Name(dir string) string {
	if dir == "" {
		return "(unknown)"
	}
	base := filepath.Base(dir)
	if base == "." || base == string(filepath.Separator) {
		return "(unknown)"
	}
	return base
}
