package claudecode

import "testing"

func TestModelRatesMatchesTheLongestFragment(t *testing.T) {
	a := &Agent{}
	r, ok := a.ModelRates("claude-opus-5-5")
	if !ok || r.Input != 4 {
		t.Errorf("opus-5-5 = %+v, %v; want the Opus 5.5 price, not Opus 5's", r, ok)
	}
	if r, ok := a.ModelRates("claude-opus-5"); !ok || r.Input != 5 {
		t.Errorf("opus-5 = %+v, %v", r, ok)
	}
	if _, ok := a.ModelRates("claude-opus-4-1"); ok {
		t.Error("an unlisted model shouldn't have a price")
	}
}

func TestCheaperModelHasAPrice(t *testing.T) {
	a := &Agent{}
	for _, m := range []string{"claude-opus-5-5", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6"} {
		name, ok := a.CheaperModel(m)
		if !ok {
			t.Errorf("%s: no cheaper model", m)
			continue
		}
		if _, ok := a.ModelRates(name); !ok {
			t.Errorf("%s suggests %s, which has no price", m, name)
		}
	}
	if _, ok := a.CheaperModel("claude-sonnet-5-5"); ok {
		t.Error("Sonnet isn't premium, so there's nothing cheaper to suggest")
	}
}
