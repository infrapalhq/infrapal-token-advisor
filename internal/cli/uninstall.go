package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runUninstall(args []string) error {
	fs := newFlagSet("uninstall")
	keepDB := fs.Bool("keep-db", false, "keep the SQLite database after uninstalling")
	agentName := fs.String("agent", "", "only disconnect this agent, keeping everything else: "+supportedAgents())
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	if *agentName != "" {
		return disconnectAgent(p, *agentName)
	}

	sp, err := servicePaths(p)
	if err != nil {
		return err
	}
	svc, err := newService(sp)
	switch {
	case errors.Is(err, service.ErrUnsupported):
		fmt.Println("no supported service manager on this platform — stop and remove the `token-advisor start` process yourself")
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
	for _, a := range connectedAgents(p.Home, cfg) {
		conn := agent.Connection{BackupPath: cfg.Agents[string(a.ID())].BackupPath}
		if err := a.Disconnect(os.Stdout, conn); err != nil {
			return err
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

// disconnectAgent removes one agent, leaving the service and data for the rest.
func disconnectAgent(p paths.Paths, name string) error {
	a, err := chooseAgent(p.Home, name)
	if err != nil {
		return err
	}
	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	conn := agent.Connection{BackupPath: cfg.Agents[string(a.ID())].BackupPath}
	if err := a.Disconnect(os.Stdout, conn); err != nil {
		return err
	}
	delete(cfg.Agents, string(a.ID()))
	if err := config.Save(p, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("%s disconnected. token-advisor is still running: `token-advisor uninstall` removes it completely\n", a.Name())
	return nil
}
