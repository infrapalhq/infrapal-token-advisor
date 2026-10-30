package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/api"
	"github.com/infrapalhq/infrapal-token-advisor/internal/logging"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/webui"
)

// startDashboard serves the dashboard alongside the collector and returns a
// function that stops it. A failure here is logged, not returned: losing the
// dashboard must never stop the collector, since a dead collector silently
// loses telemetry.
//
// It opens its own database handle because store.Open pins each one to a
// single connection, and a slow dashboard query mustn't hold up ingest.
func startDashboard(p paths.Paths, port int) func(context.Context) {
	stopped := func(context.Context) {}

	db, err := openDBAt(p)
	if err != nil {
		slog.Error("dashboard not started: open database", "err", err)
		return stopped
	}
	distFS, err := webui.DistFS()
	if err != nil {
		db.Close()
		slog.Error("dashboard not started: load embedded dashboard", "err", err)
		return stopped
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		db.Close()
		slog.Error("dashboard not started", "err", err)
		return stopped
	}

	srv := api.NewServer(addr, db, distFS)
	go func() {
		defer logging.Recover("dashboard")
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("dashboard stopped", "err", err)
		}
	}()
	fmt.Printf("token-advisor dashboard: http://%s\n", addr)

	return func(ctx context.Context) {
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutting down dashboard", "err", err)
		}
		db.Close()
	}
}

// dashboardRunning reports whether a dashboard is already answering on port,
// such as the one the background service runs.
func dashboardRunning(port int) bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
