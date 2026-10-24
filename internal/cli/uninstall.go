package cli

import (
	"fmt"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/claudesettings"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runUninstall(args []string) error {
	fs := newFlagSet("uninstall")
	keepDB := fs.Bool("keep-db", false, "keep the SQLite database after uninstalling")
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	sp, err := servicePaths(p)
	if err != nil {
		return err
	}
	svc, err := newService(sp)
	switch {
	case err == service.ErrUnsupported:
		fmt.Println("no supported service manager on this platform — stop and remove the `infrapal-token-advisor start` process yourself")
	case err != nil:
		return err
	default:
		if err := svc.Uninstall(); err != nil {
			return fmt.Errorf("uninstall background service: %w", err)
		}
		fmt.Println("background collector service stopped and removed")
	}

	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	switch {
	case cfg.SettingsBackupPath == "":
		fmt.Println("no settings.json backup recorded (init never ran, or ran before this field existed) — settings.json left as-is")
	default:
		if _, statErr := os.Stat(cfg.SettingsBackupPath); statErr != nil {
			fmt.Printf("settings.json backup %s is missing — settings.json left as-is\n", cfg.SettingsBackupPath)
		} else if err := claudesettings.Restore(p.ClaudeSettingsPath, cfg.SettingsBackupPath); err != nil {
			return fmt.Errorf("restore %s: %w", p.ClaudeSettingsPath, err)
		} else {
			fmt.Printf("restored %s from %s\n", p.ClaudeSettingsPath, cfg.SettingsBackupPath)
		}
	}

	if *keepDB {
		fmt.Printf("--keep-db set: leaving %s in place\n", p.BaseDir)
	} else {
		if err := os.RemoveAll(p.BaseDir); err != nil {
			return fmt.Errorf("remove %s: %w", p.BaseDir, err)
		}
		fmt.Printf("removed %s\n", p.BaseDir)
	}

	fmt.Println("uninstall complete")
	return nil
}
