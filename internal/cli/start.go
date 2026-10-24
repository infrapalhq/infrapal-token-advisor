package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/otlp"
)

func runStart(args []string) error {
	fs := newFlagSet("start")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, p, err := openDB()
	if n, err := ingest.BackfillProjectDirs(db); err != nil {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: project backfill: %v\n", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: resolved project directory for %d session(s)\n", n)
	}
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	writer := ingest.NewWriter(db)
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(ctx) }()

	metricsWriter := ingest.NewMetricsWriter(db)
	metricsWriterDone := make(chan error, 1)
	go func() { metricsWriterDone <- metricsWriter.Run(ctx) }()

	spansWriter := ingest.NewSpansWriter(db)
	spansWriterDone := make(chan error, 1)
	go func() { spansWriterDone <- spansWriter.Run(ctx) }()

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.OTLPPort)
	srv := otlp.NewServer(addr, writer, metricsWriter, spansWriter)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	fmt.Printf("infrapal-token-advisor collector listening on http://%s (OTLP http/protobuf: logs, metrics, traces)\n", addr)
	fmt.Printf("writing to %s\n", p.DBPath)
	fmt.Println("press Ctrl+C to stop")

	select {
	case <-ctx.Done():
		fmt.Println("\nshutting down...")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("collector server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: error shutting down server: %v\n", err)
	}

	stop() // ensure writers' context is cancelled so they drain and exit
	if err := <-writerDone; err != nil {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: error draining logs writer: %v\n", err)
	}
	if err := <-metricsWriterDone; err != nil {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: error draining metrics writer: %v\n", err)
	}
	if err := <-spansWriterDone; err != nil {
		fmt.Fprintf(os.Stderr, "infrapal-token-advisor: error draining spans writer: %v\n", err)
	}

	return nil
}
