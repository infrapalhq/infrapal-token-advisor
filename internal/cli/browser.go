package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser best-effort opens url in the user's default browser. infrapal-token-advisor
// only ships service-install support for darwin/linux (see
// internal/service), so that's the scope here too — failure is non-fatal,
// the caller just prints the URL for the user to open by hand.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("don't know how to open a browser on %s", runtime.GOOS)
	}
}
