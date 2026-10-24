package cli

import (
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

func TestServiceStatusLine(t *testing.T) {
	home := withFakeHome(t)
	fake := withFakeService(t)

	p, err := paths.Resolve()
	if err != nil {
		t.Fatalf("paths.Resolve: %v", err)
	}
	_ = home

	if got := serviceStatusLine(p); !strings.Contains(got, "not installed") {
		t.Errorf("serviceStatusLine before install = %q, want mention of not installed", got)
	}

	fake.installed = true
	fake.running = true
	if got := serviceStatusLine(p); !strings.Contains(got, "running") {
		t.Errorf("serviceStatusLine after install = %q, want mention of running", got)
	}

	fake.running = false
	if got := serviceStatusLine(p); !strings.Contains(got, "not running") {
		t.Errorf("serviceStatusLine when stopped = %q, want mention of not running", got)
	}
}
