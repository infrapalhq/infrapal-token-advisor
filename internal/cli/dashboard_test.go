package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

// freePort returns a loopback port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestStartDashboardServesAndStops(t *testing.T) {
	withFakeHome(t)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)

	stop := startDashboard(p, port)
	if !dashboardRunning(port) {
		t.Fatal("dashboard not answering after startDashboard")
	}
	stop(context.Background())
	if dashboardRunning(port) {
		t.Error("dashboard still answering after stop")
	}
}

func TestStartDashboardSurvivesPortConflict(t *testing.T) {
	withFakeHome(t)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Returns a usable stop function instead of failing the collector.
	stop := startDashboard(p, ln.Addr().(*net.TCPAddr).Port)
	stop(context.Background())
}

func TestRunUIUsesRunningDashboard(t *testing.T) {
	home := withFakeHome(t)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	stop := startDashboard(p, port)
	defer stop(context.Background())

	cfg := []byte(`{"otlp_port":4318,"api_port":` + strconv.Itoa(port) + `}`)
	if err := os.WriteFile(filepath.Join(home, ".infrapal-token-advisor", "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}

	// Would block until interrupted if it tried to serve the port itself.
	if err := runUI([]string{"--no-browser"}); err != nil {
		t.Fatalf("runUI: %v", err)
	}
}

func TestRunStartReportsDatabaseFailure(t *testing.T) {
	home := withFakeHome(t)
	// A file where the data directory should go makes opening the database fail.
	if err := os.WriteFile(filepath.Join(home, ".infrapal-token-advisor"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runStart(nil); err == nil {
		t.Fatal("runStart succeeded with no usable data directory")
	}
}
