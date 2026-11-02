package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runInit(args []string) error {
	fs := newFlagSet("init")
	rawBodies := fs.Bool("raw-bodies", false, "enable OTEL_LOG_RAW_API_BODIES in file: mode (never inline)")
	agentName := fs.String("agent", "", "the agent to connect: "+supportedAgents())
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	chosen, err := chooseAgent(p.Home, *agentName)
	if err != nil {
		return err
	}
	if err := p.EnsureBaseDir(); err != nil {
		return fmt.Errorf("create %s: %w", p.BaseDir, err)
	}

	db, err := openDBAt(p)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	db.Close()

	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.RawBodies = *rawBodies

	opts := agent.ConnectOptions{
		OTLPPort: cfg.OTLPPort,
		Previous: agent.Connection{BackupPath: cfg.Agents[string(chosen.ID())].BackupPath},
	}
	if *rawBodies {
		if err := p.EnsureBodiesDir(); err != nil {
			return fmt.Errorf("create %s: %w", p.BodiesDir, err)
		}
		opts.RawBodiesDir = p.BodiesDir
	}
	conn, err := chosen.Connect(os.Stdout, opts)
	if err != nil {
		return err
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]config.AgentState{}
	}
	cfg.Agents[string(chosen.ID())] = config.AgentState{BackupPath: conn.BackupPath}

	if err := config.Save(p, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if err := installService(p, cfg); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("data is stored at %s (0600) inside %s (0700) — this includes prompts, source fragments, shell commands, and file paths.\n", p.DBPath, p.BaseDir)
	if cfg.RetentionMonths > 0 {
		fmt.Printf("data older than %d months is deleted automatically — set retention_months in %s to change this, or 0 to keep everything.\n", cfg.RetentionMonths, p.ConfigPath)
	}

	return nil
}

// servicePaths builds the service.Paths infrapal-token-advisor's own commands need to
// dispatch to the platform-specific Service implementation.
func servicePaths(p paths.Paths) (service.Paths, error) {
	execPath, err := os.Executable()
	if err != nil {
		return service.Paths{}, fmt.Errorf("resolve own executable path: %w", err)
	}
	execPath = stableExecPath(execPath, os.Getenv("PATH"))
	return service.Paths{
		ExecPath:  execPath,
		StdoutLog: filepath.Join(p.BaseDir, "collector.log"),
		StderrLog: filepath.Join(p.BaseDir, "collector.err.log"),
		Home:      p.Home,
	}, nil
}

// stableExecPath returns the name on path that runs the same file as exe, if
// there is one. Homebrew keeps binaries in a versioned folder and links them
// into its bin directory, so a service pointing at the real file would break
// at the next upgrade, when that folder is deleted.
func stableExecPath(exe, path string) string {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "token-advisor")
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil && resolved == real {
			return candidate
		}
	}
	return exe
}

// newService is service.New behind a seam so tests can substitute a fake
// Service instead of shelling out to the real launchctl/systemctl.
var newService = service.New

// installService installs and starts the background collector service,
// printing manual instructions instead of failing on platforms with no
// supported service manager.
func installService(p paths.Paths, cfg config.Config) error {
	sp, err := servicePaths(p)
	if err != nil {
		return err
	}
	svc, err := newService(sp)
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Println("no supported service manager on this platform — run `token-advisor start` yourself (e.g. via cron @reboot or your own supervisor) to keep the collector running")
		return nil
	}
	if err != nil {
		return err
	}
	if err := svc.Install(); err != nil {
		return fmt.Errorf("install background service: %w", err)
	}
	fmt.Println("background collector service installed and started")
	fmt.Printf("dashboard: http://127.0.0.1:%d\n", cfg.APIPort)
	return nil
}
