package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/update"
)

// newUpdateClient is update.New behind a seam, so tests use a local server.
var newUpdateClient = update.New

func runUpdate(args []string) error {
	fs := newFlagSet("update")
	check := fs.Bool("check", false, "only report whether a newer release exists")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	client := newUpdateClient()
	tag, err := client.Latest(ctx)
	if err != nil {
		return fmt.Errorf("find the latest release: %w", err)
	}
	newer, err := update.Newer(tag, Version)
	if err != nil {
		return fmt.Errorf("this build (%s) can't update itself, as it wasn't installed from a release; the latest release is %s", Version, tag)
	}
	if !newer {
		fmt.Printf("token-advisor %s is up to date\n", Version)
		return nil
	}
	fmt.Printf("%s is available (this is %s)\n", tag, Version)
	if *check {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if update.ManagedBy(exe) == "homebrew" {
		return errors.New("this install is managed by Homebrew — run `brew upgrade --cask infrapal-token-advisor`, then `token-advisor restart`")
	}

	if err := client.Install(ctx, tag, runtime.GOOS, runtime.GOARCH, exe); err != nil {
		return fmt.Errorf("install %s: %w", tag, err)
	}
	fmt.Printf("installed %s to %s\n", tag, exe)

	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	if err := restartService(p); err != nil {
		fmt.Printf("the service wasn't restarted: %v\n", err)
	}
	return nil
}
