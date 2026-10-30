package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/update"
)

func withFakeRelease(t *testing.T, tag, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q}`, tag)
	}))
	t.Cleanup(srv.Close)
	origClient, origVersion := newUpdateClient, Version
	newUpdateClient = func() *update.Client {
		return &update.Client{HTTP: srv.Client(), APIBase: srv.URL, ReleaseBase: srv.URL}
	}
	Version = version
	t.Cleanup(func() { newUpdateClient, Version = origClient, origVersion })
}

func TestRunUpdateUpToDate(t *testing.T) {
	withFakeRelease(t, "v1.0.0", "1.0.0")
	if err := runUpdate(nil); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
}

func TestRunUpdateCheckChangesNothing(t *testing.T) {
	withFakeRelease(t, "v2.0.0", "1.0.0")
	if err := runUpdate([]string{"--check"}); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
}

func TestRunUpdateRefusesDevBuild(t *testing.T) {
	withFakeRelease(t, "v2.0.0", "dev")
	err := runUpdate(nil)
	if err == nil || !strings.Contains(err.Error(), "wasn't installed from a release") {
		t.Fatalf("runUpdate = %v", err)
	}
}
