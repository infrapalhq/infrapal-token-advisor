package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/claudesettings"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runInit(args []string) error {
	fs := newFlagSet("init")
	rawBodies := fs.Bool("raw-bodies", false, "enable OTEL_LOG_RAW_API_BODIES in file: mode (never inline)")
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

	backupPath, err := claudesettings.Backup(p.ClaudeSettingsPath, time.Now())
	if err != nil {
		return fmt.Errorf("back up %s: %w", p.ClaudeSettingsPath, err)
	}
	if backupPath != "" {
		fmt.Printf("backed up %s to %s\n", p.ClaudeSettingsPath, backupPath)
		// Keep the first backup across repeat inits: a later one already
		// holds our changes, so uninstall couldn't restore from it.
		if cfg.SettingsBackupPath == "" {
			cfg.SettingsBackupPath = backupPath
		}
	}

	doc, _, err := claudesettings.Load(p.ClaudeSettingsPath)
	if err != nil {
		return err
	}

	wanted := wantedEnv(cfg)
	if *rawBodies {
		if err := p.EnsureBodiesDir(); err != nil {
			return fmt.Errorf("create %s: %w", p.BodiesDir, err)
		}
		wanted["OTEL_LOG_RAW_API_BODIES"] = "file:" + p.BodiesDir
		fmt.Println("warning: --raw-bodies captures the entire conversation history and system prompt on every API request, written unredacted to disk under", p.BodiesDir)
	}

	changes, err := claudesettings.MergeEnv(doc, wanted)
	if err != nil {
		return fmt.Errorf("merge settings.json env block: %w", err)
	}

	if err := claudesettings.Save(p.ClaudeSettingsPath, doc); err != nil {
		return fmt.Errorf("write %s: %w", p.ClaudeSettingsPath, err)
	}

	if len(changes) == 0 {
		fmt.Println("settings.json already had everything token-advisor wants — nothing changed")
	} else {
		fmt.Println("changed in", p.ClaudeSettingsPath+":")
		for _, c := range changes {
			if !c.Existed {
				fmt.Printf("  %s: added (%q)\n", c.Key, c.NewValue)
			} else {
				fmt.Printf("  %s: %v -> %q\n", c.Key, c.OldValue, c.NewValue)
			}
		}
	}
	fmt.Println("reminder: already-running Claude Code sessions won't pick up the new env — restart them.")

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

// wantedEnv builds the fixed set of settings.json env keys infrapal-token-advisor's init
// wants present, per prompt.md's init specifics. Values are all strings,
// matching the JSON sample.
func wantedEnv(cfg config.Config) map[string]string {
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
		"OTEL_METRICS_EXPORTER":               "otlp",
		"OTEL_LOGS_EXPORTER":                  "otlp",
		"OTEL_TRACES_EXPORTER":                "otlp",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
		"OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT":         fmt.Sprintf("http://localhost:%d", cfg.OTLPPort),
		"OTEL_METRIC_EXPORT_INTERVAL":         "10000",
		"OTEL_LOGS_EXPORT_INTERVAL":           "2000",
		"OTEL_TRACES_EXPORT_INTERVAL":         "2000",
		"OTEL_METRICS_INCLUDE_VERSION":        "true",
		"OTEL_METRICS_INCLUDE_ENTRYPOINT":     "true",
		"OTEL_LOG_USER_PROMPTS":               "1",
		"OTEL_LOG_ASSISTANT_RESPONSES":        "1",
		"OTEL_LOG_TOOL_DETAILS":               "1",
		"OTEL_LOG_TOOL_CONTENT":               "1",
	}
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
	if err == service.ErrUnsupported {
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
