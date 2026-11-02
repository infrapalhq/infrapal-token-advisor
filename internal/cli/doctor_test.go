package cli

import (
	"testing"
)

func TestRunDoctor_RunsCleanAgainstFreshHome(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)

	// No init has run: doctor should still complete without error, just
	// reporting FAILs (no collector listening, no settings.json, etc.)
	// rather than crashing on missing state.
	if err := runDoctor(nil); err != nil {
		t.Fatalf("runDoctor on fresh $HOME: %v", err)
	}
}

func TestRunDoctor_AfterInit(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if err := runDoctor(nil); err != nil {
		t.Fatalf("runDoctor after init: %v", err)
	}
}
