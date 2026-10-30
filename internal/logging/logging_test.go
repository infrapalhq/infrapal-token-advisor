package logging

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capture points the default logger at a file for one test.
func capture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.log")
	orig := slog.Default()
	closer, err := Setup(path, slog.LevelInfo, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closer.Close()
		slog.SetDefault(orig)
	})
	return path
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
		ok   bool
	}{
		{"", slog.LevelInfo, true},
		{"DEBUG", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"loud", slog.LevelInfo, false},
	}
	for _, c := range cases {
		got, ok := ParseLevel(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseLevel(%q) = %v, %v, want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestRotationKeepsBoundedBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	w, err := newRotatingWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxSize = 100

	line := []byte(strings.Repeat("x", 59) + "\n")
	for i := 0; i < 20; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}

	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if info.Size() > 100 {
			t.Errorf("%s is %d bytes, want at most 100", p, info.Size())
		}
		if info.Mode().Perm() != fileMode {
			t.Errorf("%s has mode %v, want %v", p, info.Mode().Perm(), os.FileMode(fileMode))
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Errorf("kept more backups than maxBackups (err=%v)", err)
	}
}

func TestLinesReadsOldestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	for name, body := range map[string]string{".2": "a\n", ".1": "b\n", "": "c\n"} {
		if err := os.WriteFile(path+name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := Lines(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, ""); got != "abc" {
		t.Errorf("Lines = %q, want abc", got)
	}
}

func TestRecoverLogsStackAndPanicsAgain(t *testing.T) {
	path := capture(t)

	func() {
		defer func() {
			if recover() == nil {
				t.Error("Recover swallowed the panic")
			}
		}()
		defer Recover("writer")
		panic("boom")
	}()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"level=ERROR", "where=writer", "value=boom", "logging_test.go"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("log has no %q:\n%s", want, data)
		}
	}
}

func TestRecoverHTTPAnswers500(t *testing.T) {
	path := capture(t)
	h := RecoverHTTP("api", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	s, err := Summarize(path, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if s.Panics != 1 || s.Errors != 1 {
		t.Errorf("Summarize = %+v, want one panic and one error", s)
	}
}

func TestStartedWarnsAfterUncleanRun(t *testing.T) {
	path := capture(t)

	Started(path) // first run ever: nothing to warn about
	Stopped()
	Started(path) // the previous run stopped cleanly
	Started(path) // the previous run never stopped

	s, err := Summarize(path, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if s.Unclean != 1 {
		t.Errorf("Unclean = %d, want 1", s.Unclean)
	}
}

func TestSummarizeSkipsOldLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	recent := time.Now().Format(time.RFC3339Nano)
	body := "time=" + old + " level=ERROR msg=old\n" +
		"time=" + recent + " level=ERROR msg=new\n" +
		"time=" + recent + " level=INFO msg=fine\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Summarize(path, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Errors)
	}
}

func TestSummarizeMissingLog(t *testing.T) {
	s, err := Summarize(filepath.Join(t.TempDir(), "none.log"), time.Time{})
	if err != nil || s != (Summary{}) {
		t.Errorf("Summarize = %+v, %v, want empty", s, err)
	}
}

func TestReadable(t *testing.T) {
	if got := Readable(`stack="main.go\n\tline 1"`); got != "stack=\"main.go\n\tline 1\"" {
		t.Errorf("Readable = %q", got)
	}
}

func TestTrimFileKeepsTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.log")
	big := append(bytes.Repeat([]byte("x"), maxFileSize), []byte("last line\n")...)
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrimFile(path); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != trimKeep {
		t.Errorf("size = %d, want %d", len(data), trimKeep)
	}
	if !bytes.HasSuffix(data, []byte("last line\n")) {
		t.Error("lost the end of the file")
	}
}

func TestTrimFileLeavesSmallAndMissingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.log")
	if err := TrimFile(path); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if err := os.WriteFile(path, []byte("small\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrimFile(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "small\n" {
		t.Errorf("file changed: %q", data)
	}
}
