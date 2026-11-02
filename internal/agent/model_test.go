package agent

import "testing"

func TestTokens_TotalAndContext(t *testing.T) {
	tok := Tokens{Input: 100, Output: 20, CacheRead: 3000, CacheWrite: 400}
	if got := tok.Total(); got != 3520 {
		t.Errorf("Total = %d, want 3520", got)
	}
	if got := tok.Context(); got != 3500 {
		t.Errorf("Context = %d, want 3500: output isn't something the model read", got)
	}
}

func TestInputWithoutCached(t *testing.T) {
	cases := []struct{ input, cached, want int64 }{
		{1000, 800, 200},
		{1000, 0, 1000},
		{800, 800, 0},
		{100, 500, 0}, // a bad count mustn't produce negative tokens
	}
	for _, c := range cases {
		if got := InputWithoutCached(c.input, c.cached); got != c.want {
			t.Errorf("InputWithoutCached(%d, %d) = %d, want %d", c.input, c.cached, got, c.want)
		}
	}
}

func TestCapability_Has(t *testing.T) {
	have := CapFilePaths | CapCompaction
	if !have.Has(CapFilePaths) {
		t.Error("should have CapFilePaths")
	}
	if !have.Has(CapFilePaths | CapCompaction) {
		t.Error("should have both")
	}
	if have.Has(CapCommands) {
		t.Error("shouldn't have CapCommands")
	}
	if have.Has(CapFilePaths | CapCommands) {
		t.Error("a check needing two things must have both")
	}
	if !have.Has(0) {
		t.Error("a check needing nothing always runs")
	}
}

func TestCapabilities_AreDistinct(t *testing.T) {
	all := []Capability{CapFilePaths, CapCommands, CapApprovalWait, CapCompaction, CapContextSize, CapPromptText, CapMCPConnections}
	seen := Capability(0)
	for _, c := range all {
		if seen&c != 0 {
			t.Errorf("capability %d overlaps another", c)
		}
		seen |= c
	}
}

func TestEvents_ShareTheirMeta(t *testing.T) {
	m := Meta{SessionID: "s", PromptID: "p", Ts: 5}
	events := []Event{
		Session{Meta: m},
		Prompt{Meta: m},
		Response{Meta: m},
		Request{Meta: m},
		TurnCost{Meta: m},
		Tool{Meta: m},
		Compaction{Meta: m},
		MCPConnection{Meta: m},
	}
	for _, e := range events {
		if e.Base() != m {
			t.Errorf("%T.Base() = %+v, want %+v", e, e.Base(), m)
		}
	}
}
