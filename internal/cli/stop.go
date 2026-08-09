package cli

import (
	"fmt"

	"github.com/ShamikMukhopadhyay/ccotel/internal/paths"
	"github.com/ShamikMukhopadhyay/ccotel/internal/service"
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
		fmt.Println("no supported service manager on this platform — kill the `ccotel start` process yourself")
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
