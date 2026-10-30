package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRestartNeedsInstalledService(t *testing.T) {
	withFakeHome(t)
	fake := withFakeService(t)

	err := runRestart(nil)
	if err == nil || !strings.Contains(err.Error(), "token-advisor init") {
		t.Fatalf("runRestart = %v, want an error pointing at init", err)
	}
	if fake.installCalls != 0 {
		t.Error("started a service that was never installed")
	}
}

func TestRunRestartReinstalls(t *testing.T) {
	withFakeHome(t)
	fake := withFakeService(t)
	fake.installed = true

	if err := runRestart(nil); err != nil {
		t.Fatalf("runRestart: %v", err)
	}
	if fake.installCalls != 1 {
		t.Errorf("installCalls = %d, want 1", fake.installCalls)
	}
}

func TestStableExecPath(t *testing.T) {
	root := t.TempDir()
	cask := filepath.Join(root, "Caskroom", "1.0")
	bin := filepath.Join(root, "bin")
	other := filepath.Join(root, "other")
	for _, dir := range []string{cask, bin, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(cask, "token-advisor")
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "token-advisor")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// A different binary of the same name earlier on PATH isn't this one.
	if err := os.WriteFile(filepath.Join(other, "token-advisor"), []byte("y"), 0o755); err != nil {
		t.Fatal(err)
	}

	path := other + string(os.PathListSeparator) + bin
	if got := stableExecPath(real, path); got != link {
		t.Errorf("stableExecPath = %q, want the link %q", got, link)
	}
	if got := stableExecPath(real, other); got != real {
		t.Errorf("not on PATH: got %q, want %q", got, real)
	}
}
