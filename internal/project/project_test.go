package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrefersTranscriptCWD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-Users-x-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"summary"}` + "\n" + `{"cwd":"/Users/x/repo","sessionId":"s1"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(root, "s1", []string{"/elsewhere/a.go"}); got != "/Users/x/repo" {
		t.Fatalf("transcript cwd should win, got %q", got)
	}
}

func TestResolveFallsBackToGitRootOfFiles(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "modules", "db")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(src, "main.tf"), filepath.Join(src, "variables.tf")}
	if got := Resolve(t.TempDir(), "missing", files); got != repo {
		t.Fatalf("want git root %q, got %q", repo, got)
	}
}

func TestNoRepoMeansUnknownNotCommonParent(t *testing.T) {
	files := []string{"/usr/local/bin/tailscale", "/Users/x/notes/a.md"}
	if got := fromFilePaths(files); got != "" {
		t.Fatalf("files in no repository must stay unknown, got %q", got)
	}
}

func TestClaudeOwnFilesAreIgnored(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join(repo, "main.tf"), "/Users/x/.claude/plans/p.md", "/Users/x/.claude/plans/q.md"}
	if got := fromFilePaths(files); got != repo {
		t.Fatalf("~/.claude paths must not vote; want %q got %q", repo, got)
	}
}

func TestResolveNothingKnown(t *testing.T) {
	if got := Resolve("", "s", nil); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestResolveRejectsTraversalSessionID(t *testing.T) {
	if got := transcriptCWD(t.TempDir(), "../etc"); got != "" {
		t.Fatalf("traversal id must be rejected, got %q", got)
	}
}

func TestName(t *testing.T) {
	if Name("/Users/x/infra-repo") != "infra-repo" {
		t.Fatal("basename expected")
	}
	if Name("") != "(unknown)" || Name("/") != "(unknown)" {
		t.Fatal("unknown placeholder expected")
	}
}
