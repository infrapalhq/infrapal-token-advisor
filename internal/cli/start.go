package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
	"github.com/infrapalhq/infrapal-token-advisor/internal/otlp"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

func runStart(args []string) error {
	fs := newFlagSet("start")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if err := p.EnsureBaseDir(); err != nil {
		return fmt.Errorf("create %s: %w", p.BaseDir, err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	level, levelOK := logging.ParseLevel(cfg.LogLevel)
	logFile, err := logging.Setup(p.LogPath, level, consoleWriter())
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()
	defer logging.Recover("collector")
	if !levelOK {
		slog.Warn("unknown log_level in config.json, using info", "log_level", cfg.LogLevel)
	}
	if sp, err := servicePaths(p); err == nil {
		for _, path := range []string{sp.StdoutLog, sp.StderrLog} {
			if err := logging.TrimFile(path); err != nil {
				slog.Warn("couldn't trim service output", "path", path, "err", err)
			}
		}
	}

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.OTLPPort)
	logging.Started(p.LogPath, "version", Version, "otlp", addr)

	db, err := openDBAt(p)
	if err != nil {
		slog.Error("open database", "err", err)
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if n, err := ingest.BackfillProjectDirs(db); err != nil {
		slog.Error("project backfill", "err", err)
	} else if n > 0 {
		slog.Info("resolved project directories", "sessions", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	writer := ingest.NewWriter(db)
	writerDone := make(chan error, 1)
	go func() {
		defer logging.Recover("logs writer")
		writerDone <- writer.Run(ctx)
	}()

	metricsWriter := ingest.NewMetricsWriter(db)
	metricsWriterDone := make(chan error, 1)
	go func() {
		defer logging.Recover("metrics writer")
		metricsWriterDone <- metricsWriter.Run(ctx)
	}()

	spansWriter := ingest.NewSpansWriter(db)
	spansWriterDone := make(chan error, 1)
	go func() {
		defer logging.Recover("spans writer")
		spansWriterDone <- spansWriter.Run(ctx)
	}()

	srv := otlp.NewServer(addr, writer, metricsWriter, spansWriter)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	fmt.Printf("token-advisor collector listening on http://%s (OTLP http/protobuf: logs, metrics, traces)\n", addr)
	fmt.Printf("writing to %s\n", p.DBPath)
	stopDashboard := startDashboard(p, cfg.APIPort)
	fmt.Println("press Ctrl+C to stop")

	select {
	case <-ctx.Done():
		fmt.Println("\nshutting down...")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("collector server", "err", err)
			return fmt.Errorf("collector server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopDashboard(shutdownCtx)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutting down server", "err", err)
	}

	stop() // ensure writers' context is cancelled so they drain and exit
	if err := <-writerDone; err != nil {
		slog.Error("draining writer", "signal", "logs", "err", err)
	}
	if err := <-metricsWriterDone; err != nil {
		slog.Error("draining writer", "signal", "metrics", "err", err)
	}
	if err := <-spansWriterDone; err != nil {
		slog.Error("draining writer", "signal", "spans", "err", err)
	}

	logging.Stopped()
	return nil
}

// consoleWriter returns stderr when it's a terminal, so a foreground run shows
// its log. Under the service it's a file, which the log mustn't fill again.
func consoleWriter() io.Writer {
	if info, err := os.Stderr.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return os.Stderr
	}
	return nil
}
