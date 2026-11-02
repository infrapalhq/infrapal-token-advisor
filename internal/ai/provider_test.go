package ai

import "testing"

func TestProviders_AreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Providers() {
		if p.ID == "" || p.Name == "" || p.Model == "" || p.KeyEnv == "" || p.New == nil {
			t.Errorf("incomplete provider: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("two providers use the ID %q", p.ID)
		}
		seen[p.ID] = true
		if p.New("key") == nil {
			t.Errorf("%s built no Requester", p.ID)
		}
	}
}

func TestFindProvider(t *testing.T) {
	if got := FindProvider("").ID; got != "anthropic" {
		t.Errorf("default = %q", got)
	}
	if got := FindProvider("anthropic").Name; got != "Anthropic" {
		t.Errorf("name = %q", got)
	}
	if got := FindProvider("no-such-provider").ID; got != "anthropic" {
		t.Errorf("unknown = %q, want the default", got)
	}
}
