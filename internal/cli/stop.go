package cli

import (
	"fmt"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func runStop(args []string) error {
	fs := newFlagSet("stop")
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
	if err == service.ErrUnsupported {
		fmt.Println("no supported service manager on this platform — kill the `infrapal-token-advisor start` process yourself")
		return nil
	}
	if err != nil {
		return err
	}
	if err := svc.Stop(); err != nil {
		return fmt.Errorf("stop background service: %w", err)
	}
	fmt.Println("background collector service stopped")
	return nil
}
