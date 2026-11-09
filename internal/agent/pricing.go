package agent

// Rates is a model's list price in USD per million tokens.
type Rates struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Cost prices a request's tokens at these rates.
func (r Rates) Cost(t Tokens) float64 {
	return (float64(t.Input)*r.Input +
		float64(t.Output)*r.Output +
		float64(t.CacheRead)*r.CacheRead +
		float64(t.CacheWrite)*r.CacheWrite) / 1e6
}

// ModelPricer is for an agent that knows its models' list prices. It's
// optional: without it, findings that need prices fall back to the install's
// blended rate or show no dollar figure.
//
// Findings use list prices only as ratios against what a request actually
// cost, so discounts, fast mode and long cache lifetimes carry through.
type ModelPricer interface {
	// ModelRates gives a model's list price. ok is false for a model it
	// doesn't know.
	ModelRates(model string) (r Rates, ok bool)
	// CheaperModel names the model to suggest instead of a premium one. ok is
	// false when there's no cheaper model with a known price.
	CheaperModel(model string) (name string, ok bool)
}
