package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/api"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/webui"
)

// runUI starts the read-only HTTP API server plus the embedded dashboard,
// and opens it in the user's default browser.
func runUI(args []string) error {
	fs := newFlagSet("ui")
	noBrowser := fs.Bool("no-browser", false, "don't try to open the dashboard in a browser")
	port := fs.Int("port", 0, "override the API port from config.json, without persisting it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, p, err := openDB()
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

	distFS, err := webui.DistFS()
	if err != nil {
		return fmt.Errorf("load embedded dashboard: %w", err)
	}

	apiPort := cfg.APIPort
	if *port != 0 {
		apiPort = *port
	}
	addr := fmt.Sprintf("127.0.0.1:%d", apiPort)
	srv := api.NewServer(addr, db, distFS)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	url := fmt.Sprintf("http://%s", addr)
	fmt.Printf("token-advisor dashboard: %s\n", url)
	fmt.Println("API endpoints:")
	for _, ep := range api.Endpoints() {
		method, path, ok := strings.Cut(ep, " ")
		if !ok {
			method, path = "", ep
		}
		if method == "" || method == "GET" {
			fmt.Printf("  http://%s%s\n", addr, path)
		} else {
			fmt.Printf("  %s http://%s%s\n", method, addr, path)
		}
	}
	fmt.Println("press Ctrl+C to stop")

	if !*noBrowser {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "token-advisor: couldn't open a browser automatically (%v) — open %s yourself\n", err, url)
		}
	}

	select {
	case <-ctx.Done():
		fmt.Println("\nshutting down...")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "token-advisor: error shutting down server: %v\n", err)
	}

	return nil
}
