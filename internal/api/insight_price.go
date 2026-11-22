package api

import (
	"sort"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// Pricing for avoidable spend. Agents report a request's total cost, not the
// price of each kind of token, so the prices are worked out from this
// install's own requests using the usual price structure: relative to fresh
// input, output costs 5×, writing to the prompt cache 1.25× and reading from
// it 0.1×. That gives one "unit" price per model (a fresh input token) that
// the four kinds of token are scaled from.
const (
	priceOutput     = 5.0
	priceCacheWrite = 1.25
	priceCacheRead  = 0.1

	// smallerModelPriceRatio is what a turn is assumed to cost on the
	// smaller tier when this install has never used one to measure.
	smallerModelPriceRatio = 0.6
)

// weighted is the request's tokens in fresh-input equivalents.
func (r reqRow) weighted() float64 {
	return float64(r.Input) + priceOutput*float64(r.Output) +
		priceCacheWrite*float64(r.CacheCreate) + priceCacheRead*float64(r.CacheRead)
}

// priceIndex is what pricing needs, built once per insightData.
type priceIndex struct {
	unit        map[string]float64   // model -> USD per fresh input token
	overall     float64              // the same across every model, for a model with no figure
	main        map[string][]reqRow  // session -> its main-thread requests, by time
	compactions map[string][]int64   // session -> compaction times, ascending
	smaller     map[agent.ID]float64 // agent -> unit rate of its priciest smaller model, built on use
}

// prices builds the index on first use. loadInsightData's views share the
// same sessions, so an index built from any of them prices a session the same.
func (d *insightData) prices() *priceIndex {
	if d.priceIdx != nil {
		return d.priceIdx
	}
	p := &priceIndex{unit: map[string]float64{}, main: map[string][]reqRow{}, compactions: map[string][]int64{}}
	cost, weight := map[string]float64{}, map[string]float64{}
	var allCost, allWeight float64
	for _, r := range d.reqs {
		w := r.weighted()
		cost[r.Model] += r.CostUSD
		weight[r.Model] += w
		allCost += r.CostUSD
		allWeight += w
		if agent.IsMainQuerySource(r.QuerySource) {
			p.main[r.SessionID] = append(p.main[r.SessionID], r)
		}
	}
	for m, w := range weight {
		if w > 0 && cost[m] > 0 {
			p.unit[m] = cost[m] / w
		}
	}
	if allWeight > 0 {
		p.overall = allCost / allWeight
	}
	for sid := range p.main {
		rs := p.main[sid]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Ts < rs[j].Ts })
	}
	for _, c := range d.compactions {
		p.compactions[c.SessionID] = append(p.compactions[c.SessionID], c.Ts)
	}
	for sid := range p.compactions {
		sort.Slice(p.compactions[sid], func(i, j int) bool { return p.compactions[sid][i] < p.compactions[sid][j] })
	}
	d.priceIdx = p
	return p
}

// unitRate is a model's price per fresh input token, or the install's
// average when the model has no cost of its own.
func (p *priceIndex) unitRate(model string) float64 {
	if u, ok := p.unit[model]; ok {
		return u
	}
	return p.overall
}

// carryUSD prices tokens that went into a session's context at ts and didn't
// need to. They aren't paid for once: the next request writes them to the
// prompt cache, and every request after that reads them again, until a
// compaction summarises them away or the session ends. Each request is priced
// at its own model's rate. With no later request, they were sent once as
// input. Zero means there is no price to give.
func (d *insightData) carryUSD(sessionID string, ts, tokens int64) float64 {
	if tokens <= 0 {
		return 0
	}
	p := d.prices()
	end := int64(1) << 62
	for _, c := range p.compactions[sessionID] {
		if c > ts {
			end = c
			break
		}
	}
	rs := p.main[sessionID]
	i := sort.Search(len(rs), func(i int) bool { return rs[i].Ts > ts })
	tok := float64(tokens)
	if i >= len(rs) || rs[i].Ts >= end {
		return tok * p.overall
	}
	usd := tok * p.unitRate(rs[i].Model) * priceCacheWrite
	for _, r := range rs[i+1:] {
		if r.Ts >= end {
			break
		}
		usd += tok * p.unitRate(r.Model) * priceCacheRead
	}
	return usd
}

// smallerUnitRate is the price per fresh input token of the most expensive,
// so most capable, smaller model this agent's sessions used, or 0 when they
// used none.
func (d *insightData) smallerUnitRate(id agent.ID) float64 {
	p := d.prices()
	if p.smaller == nil {
		p.smaller = map[agent.ID]float64{}
		for _, r := range d.reqs {
			if d.premiumModel(r) {
				continue
			}
			a := d.sessions[r.SessionID].Agent
			p.smaller[a] = max(p.smaller[a], p.unit[r.Model])
		}
	}
	return p.smaller[id]
}

// smallerModelSaving is what a premium-model request would have cost less on
// the agent's smaller tier: priced from this install's own smaller-model
// requests, or by smallerModelPriceRatio when there are none to price from.
func (d *insightData) smallerModelSaving(r reqRow) float64 {
	premium := d.prices().unitRate(r.Model)
	alt := d.smallerUnitRate(d.sessions[r.SessionID].Agent)
	if alt <= 0 || alt >= premium {
		return r.CostUSD * (1 - smallerModelPriceRatio)
	}
	return max(r.CostUSD-r.weighted()*alt, 0)
}
