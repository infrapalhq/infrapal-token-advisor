package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrintLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	writeLog(t, path,
		`time=2026-10-30T10:00:00Z level=INFO msg=one`,
		`time=2026-10-30T10:00:01Z level=ERROR msg=two stack="a\n\tb"`,
		`time=2026-10-30T10:00:02Z level=INFO msg=three`,
	)

	var all bytes.Buffer
	if err := printLogTail(&all, path, 2, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(all.String(), "msg=one") || !strings.Contains(all.String(), "msg=three") {
		t.Errorf("-n 2 output:\n%s", all.String())
	}

	var errs bytes.Buffer
	if err := printLogTail(&errs, path, 100, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errs.String(), "msg=three") || !strings.Contains(errs.String(), "a\n\tb") {
		t.Errorf("--errors output:\n%s", errs.String())
	}
}

func TestPrintLogTailNoLog(t *testing.T) {
	var out bytes.Buffer
	if err := printLogTail(&out, filepath.Join(t.TempDir(), "none.log"), 10, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no log yet") {
		t.Errorf("output: %q", out.String())
	}
}

// syncBuffer lets the test read what followLog writes from its goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestFollowLogShowsNewLinesAfterRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	writeLog(t, path, "old line one", "old line two")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	offset := logSize(path)
	go func() { done <- followLog(ctx, out, path, offset, false) }()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(out.String(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %q in:\n%s", want, out.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("appended\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	waitFor("appended")

	writeLog(t, path, "rotated") // shorter than before, as after a rotation
	waitFor("rotated")

	if strings.Contains(out.String(), "old line") {
		t.Errorf("repeated old lines:\n%s", out.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
