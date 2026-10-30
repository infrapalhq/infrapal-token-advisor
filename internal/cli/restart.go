package cli

import (
	"errors"
	"fmt"

	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runRestart(args []string) error {
	fs := newFlagSet("restart")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	return restartService(p)
}

// restartService reinstalls the service, which also restarts it on the current
// binary. It's refused on an install with no service, so that a restart can't
// start collecting for someone who never ran init.
func restartService(p paths.Paths) error {
	sp, err := servicePaths(p)
	if err != nil {
		return err
	}
	svc, err := newService(sp)
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Println("no supported service manager on this platform — restart the `token-advisor start` process yourself")
		return nil
	}
	if err != nil {
		return err
	}
	st, err := svc.Status()
	if err != nil {
		return err
	}
	if !st.Installed {
		return errors.New("the background service isn't installed — run `token-advisor init`")
	}
	cfg, err := config.Load(p)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return installService(p, cfg)
}
