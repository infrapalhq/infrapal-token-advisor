package ai

import "testing"

func sampleBundle() Context {
	return Context{
		TopPrompts: []PromptFact{
			{PromptID: "p1", TotalCostUSD: 7.00, TotalTokens: 1500, LLMTurnCount: 2},
		},
		Tools: []ToolFact{
			{ToolName: "Bash", TotalCalls: 12},
		},
		Rollups: Rollups{
			TotalWastedSpendUSD:    42.10,
			TotalRejectedToolCalls: 3,
		},
	}
}

func TestValidateEvidence_ExactNumberGrounds(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "efficiency", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "cost", Value: "7"}}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d, want 1/0", len(kept), len(dropped))
	}
}

func TestValidateEvidence_ReformattedNumberWithinToleranceGrounds(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "waste", Severity: "warn", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "wasted", Value: "$42.10"}}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d, want 1/0", len(kept), len(dropped))
	}
}

func TestValidateEvidence_FabricatedNumberDropped(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "efficiency", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "cost", Value: "9999.99"}}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d, want 0/1", len(kept), len(dropped))
	}
}

func TestValidateEvidence_KnownNameGrounds(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "tools", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "tool", Value: "Bash"}}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 1 || len(dropped) != 0 {
		t.Fatalf("kept=%d dropped=%d, want 1/0", len(kept), len(dropped))
	}
}

func TestValidateEvidence_UnrelatedStringDropped(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "tools", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "tool", Value: "PowerShell"}}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d, want 0/1", len(kept), len(dropped))
	}
}

func TestValidateEvidence_OneUngroundedFactDropsWholeRecommendation(t *testing.T) {
	bundle := sampleBundle()
	recs := []Recommendation{
		{Category: "efficiency", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{
				{Label: "cost", Value: "7"},         // grounds
				{Label: "made up", Value: "123456"}, // does not
			}},
	}
	kept, dropped := ValidateEvidence(bundle, recs)
	if len(kept) != 0 || len(dropped) != 1 {
		t.Fatalf("kept=%d dropped=%d, want 0/1 (partial grounding still drops the whole recommendation)", len(kept), len(dropped))
	}
}
