package claudecode

import (
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// modelPrice is one model family's list price, matched by a fragment of the
// model ID. Cache writes are the 5-minute rate, 1.25× input.
type modelPrice struct {
	fragment string
	rates    agent.Rates
	cheaper  string // the fragment of the model to suggest instead, if any
}

// prices is checked in order, so a longer fragment must come before any
// fragment it contains ("opus-5-5" before "opus-5").
var prices = []modelPrice{
	{"opus-5-5", agent.Rates{Input: 4, Output: 20, CacheRead: 0.20, CacheWrite: 5}, "sonnet-5-5"},
	{"opus-5", agent.Rates{Input: 5, Output: 25, CacheRead: 0.50, CacheWrite: 6.25}, "sonnet-5"},
	{"opus-4-8", agent.Rates{Input: 5, Output: 25, CacheRead: 0.50, CacheWrite: 6.25}, "sonnet-4-6"},
	{"opus-4-7", agent.Rates{Input: 5, Output: 25, CacheRead: 0.50, CacheWrite: 6.25}, "sonnet-4-6"},
	{"opus-4-6", agent.Rates{Input: 5, Output: 25, CacheRead: 0.50, CacheWrite: 6.25}, "sonnet-4-6"},
	{"sonnet-5-5", agent.Rates{Input: 2, Output: 10, CacheRead: 0.20, CacheWrite: 2.5}, ""},
	{"sonnet-5", agent.Rates{Input: 2, Output: 10, CacheRead: 0.20, CacheWrite: 2.5}, ""},
	{"sonnet-4-6", agent.Rates{Input: 3, Output: 15, CacheRead: 0.30, CacheWrite: 3.75}, ""},
}

func priceFor(model string) (modelPrice, bool) {
	for _, p := range prices {
		if strings.Contains(model, p.fragment) {
			return p, true
		}
	}
	return modelPrice{}, false
}

// ModelRates implements agent.ModelPricer.
func (a *Agent) ModelRates(model string) (agent.Rates, bool) {
	p, ok := priceFor(model)
	return p.rates, ok
}

// CheaperModel implements agent.ModelPricer.
func (a *Agent) CheaperModel(model string) (string, bool) {
	p, ok := priceFor(model)
	if !ok || p.cheaper == "" {
		return "", false
	}
	return "claude-" + p.cheaper, true
}
