// Package logging sets up the collector's log file: structured lines through
// log/slog, rotated by size so a service that runs for months can't fill the
// disk. Lines must never include prompts, responses or tool content; log
// counts, ids and error messages instead.
package logging

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	// maxFileSize is the size at which the log file is rotated.
	maxFileSize = 10 << 20
	// maxBackups is how many rotated files are kept, as <path>.1 (newest)
	// through <path>.3. With the live file that bounds logs at 40 MiB.
	maxBackups = 3
	// trimKeep is how much of a service output file TrimFile keeps.
	trimKeep = 1 << 20
	fileMode = 0o600

	msgStarted = "collector started"
	msgStopped = "collector stopped"
	msgPanic   = "panic"
)

// ParseLevel maps a config value to a slog level. Unknown values give the
// default, info, and false.
func ParseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, true
	case "", "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// Setup sends the default slog logger, and through it the log package, to the
// rotating file at path, and to console as well if it isn't nil. Close the
// result on exit.
func Setup(path string, level slog.Level, console io.Writer) (io.Closer, error) {
	w, err := newRotatingWriter(path)
	if err != nil {
		return nil, err
	}
	var out io.Writer = w
	if console != nil {
		out = io.MultiWriter(w, console)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})))
	return w, nil
}

// Started records a collector start, first warning if the previous run ended
// without Stopped: a crash, a kill or a power loss.
func Started(path string, attrs ...any) {
	if !lastRunClean(path) {
		slog.Warn("previous run did not shut down cleanly")
	}
	slog.Info(msgStarted, attrs...)
}

// Stopped records a clean collector shutdown.
func Stopped() {
	slog.Info(msgStopped)
}

// lastRunClean reports whether the last run on record finished. A log with
// no runs in it counts as clean.
func lastRunClean(path string) bool {
	lines, err := Lines(path)
	if err != nil {
		return true
	}
	for i := len(lines) - 1; i >= 0; i-- {
		switch {
		case strings.Contains(lines[i], `msg="`+msgStopped+`"`):
			return true
		case strings.Contains(lines[i], `msg="`+msgStarted+`"`):
			return false
		}
	}
	return true
}

// Recover is deferred at the top of a goroutine. It logs a panic with its
// stack, then panics again: the state is unknown, so the process should die
// and let the service manager restart it.
func Recover(where string) {
	if r := recover(); r != nil {
		logPanic(where, r)
		panic(r)
	}
}

// RecoverHTTP keeps a panic in one request from losing its stack or taking
// other requests with it: it logs the panic and answers 500.
func RecoverHTTP(where string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			logPanic(where, rec)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

func logPanic(where string, r any) {
	slog.Error(msgPanic, "where", where, "value", fmt.Sprint(r), "stack", string(debug.Stack()))
}

// Summary counts what went wrong in the log since a point in time.
type Summary struct {
	Errors  int
	Panics  int
	Unclean int
}

// Summarize reads the log files at path. A missing log is an empty summary.
func Summarize(path string, since time.Time) (Summary, error) {
	lines, err := Lines(path)
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	for _, line := range lines {
		ts, ok := lineTime(line)
		if !ok || ts.Before(since) {
			continue
		}
		if IsError(line) {
			s.Errors++
		}
		if strings.Contains(line, "msg="+msgPanic+" ") {
			s.Panics++
		}
		if strings.Contains(line, `msg="previous run did not shut down cleanly"`) {
			s.Unclean++
		}
	}
	return s, nil
}

// IsError reports whether a log line is at error level.
func IsError(line string) bool {
	return strings.Contains(line, " level=ERROR ")
}

func lineTime(line string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(line, "time=")
	if !ok {
		return time.Time{}, false
	}
	stamp, _, _ := strings.Cut(rest, " ")
	ts, err := time.Parse(time.RFC3339Nano, stamp)
	return ts, err == nil
}

// Readable turns a log line's escaped newlines and tabs back into real ones,
// so a stack trace reads as one.
func Readable(line string) string {
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t").Replace(line)
}

// Lines returns every log line at path, oldest first, across rotated files.
func Lines(path string) ([]string, error) {
	var lines []string
	for i := maxBackups; i >= 0; i-- {
		file := path
		if i > 0 {
			file = backupPath(path, i)
		}
		got, err := readLines(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		lines = append(lines, got...)
	}
	return lines, nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if line = strings.TrimRight(line, "\n"); line != "" {
			lines = append(lines, line)
		}
		if err == io.EOF {
			return lines, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// TrimFile keeps a service output file from growing forever, cutting it to
// its last part once it passes maxFileSize. The service manager owns these
// files, so they can't be rotated; it appends, so shrinking one is safe. Call
// it at startup.
func TrimFile(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= maxFileSize {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tail := make([]byte, trimKeep)
	if _, err := f.ReadAt(tail, info.Size()-trimKeep); err != nil {
		return err
	}
	return os.WriteFile(path, tail, fileMode)
}

func backupPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

// rotatingWriter appends to path, moving it aside once it reaches maxSize.
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
	size    int64
}

func newRotatingWriter(path string) (*rotatingWriter, error) {
	w := &rotatingWriter{path: path, maxSize: maxFileSize}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *rotatingWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(b)) > w.maxSize {
		// A failed rotation shouldn't lose the line, so keep the old file.
		if err := w.rotate(); err != nil {
			fmt.Fprintf(os.Stderr, "token-advisor: rotate log: %v\n", err)
		}
	}
	n, err := w.file.Write(b)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	for i := maxBackups - 1; i >= 1; i-- {
		err := os.Rename(backupPath(w.path, i), backupPath(w.path, i+1))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(err, w.open())
		}
	}
	if err := os.Rename(w.path, backupPath(w.path, 1)); err != nil {
		return errors.Join(err, w.open())
	}
	return w.open()
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
