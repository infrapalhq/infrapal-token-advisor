package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

const logsPollInterval = 500 * time.Millisecond

func runLogs(args []string) error {
	fs := newFlagSet("logs")
	n := fs.Int("n", 100, "how many lines to show")
	errorsOnly := fs.Bool("errors", false, "show only errors and crashes")
	follow := fs.Bool("f", false, "keep showing new lines until interrupted")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	// Measured before printing, so a line written in between isn't missed.
	offset := logSize(p.LogPath)
	if err := printLogTail(os.Stdout, p.LogPath, *n, *errorsOnly); err != nil {
		return err
	}
	if !*follow {
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return followLog(ctx, os.Stdout, p.LogPath, offset, *errorsOnly)
}

// logSize returns the log's current size, or 0 if there isn't one yet.
func logSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// printLogTail writes the last n matching lines, across rotated files.
func printLogTail(w io.Writer, path string, n int, errorsOnly bool) error {
	lines, err := logging.Lines(path)
	if err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	if len(lines) == 0 {
		fmt.Fprintf(w, "no log yet at %s — the collector writes one once it starts\n", path)
		return nil
	}
	var matched []string
	for _, line := range lines {
		if !errorsOnly || logging.IsError(line) {
			matched = append(matched, line)
		}
	}
	if len(matched) > n {
		matched = matched[len(matched)-n:]
	}
	for _, line := range matched {
		fmt.Fprintln(w, logging.Readable(line))
	}
	return nil
}

// followLog prints lines added to path after offset until ctx ends. A file
// smaller than what's been read has been rotated, so it starts again from the
// top.
func followLog(ctx context.Context, w io.Writer, path string, offset int64, errorsOnly bool) error {
	var partial string
	ticker := time.NewTicker(logsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Size() < offset {
			offset, partial = 0, ""
		}
		if info.Size() == offset {
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data := make([]byte, info.Size()-offset)
		_, err = f.ReadAt(data, offset)
		f.Close()
		if err != nil {
			return err
		}
		offset += int64(len(data))

		lines := strings.Split(partial+string(data), "\n")
		partial = lines[len(lines)-1] // an unfinished line waits for the next read
		for _, line := range lines[:len(lines)-1] {
			if line != "" && (!errorsOnly || logging.IsError(line)) {
				fmt.Fprintln(w, logging.Readable(line))
			}
		}
	}
}
