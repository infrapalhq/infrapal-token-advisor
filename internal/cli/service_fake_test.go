package cli

import "github.com/infrapalhq/infrapal-token-advisor/internal/service"

// fakeService is an in-memory service.Service double for CLI tests, so
// runInit/runStop/runUninstall/runStatus can be exercised end-to-end without
// shelling out to the real launchctl/systemctl on the machine running tests.
type fakeService struct {
	installed      bool
	running        bool
	installCalls   int
	uninstallCalls int
	startCalls     int
	stopCalls      int
	installErr     error
	uninstallErr   error
}

func (f *fakeService) Install() error {
	f.installCalls++
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = true
	f.running = true
	return nil
}

func (f *fakeService) Uninstall() error {
	f.uninstallCalls++
	if f.uninstallErr != nil {
		return f.uninstallErr
	}
	f.installed = false
	f.running = false
	return nil
}

func (f *fakeService) Start() error {
	f.startCalls++
	f.running = true
	return nil
}

func (f *fakeService) Stop() error {
	f.stopCalls++
	f.running = false
	return nil
}

func (f *fakeService) Status() (service.Status, error) {
	detail := "not loaded"
	if f.running {
		detail = "running (pid 4242)"
	} else if f.installed {
		detail = "loaded, not running"
	}
	return service.Status{Installed: f.installed, Running: f.running, Detail: detail}, nil
}

// withFakeService swaps newService for the duration of a test, returning the
// fake so the test can assert on it, and restores the real one on cleanup.
func withFakeService(t interface{ Cleanup(func()) }) *fakeService {
	fake := &fakeService{}
	orig := newService
	newService = func(service.Paths) (service.Service, error) { return fake, nil }
	t.Cleanup(func() { newService = orig })
	return fake
}
