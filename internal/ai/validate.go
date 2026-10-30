package ai

import (
	"log/slog"
	"math"
	"strconv"
	"strings"
)

// DroppedRecommendation is a model-proposed Recommendation that failed
// grounding — kept around only for logging, never returned to a caller.
type DroppedRecommendation struct {
	Recommendation Recommendation
	Reason         string
}

// ValidateEvidence checks every Recommendation's evidence facts against the
// flattened set of known values actually present in bundle — the data that
// was actually sent to Anthropic. A Recommendation is dropped in full, not
// partially edited, if any one evidence fact fails to ground: a
// partially-fabricated recommendation is exactly as untrustworthy as a
// fully-fabricated one. This is the second anti-hallucination layer, behind
// the system prompt's instructions.
func ValidateEvidence(bundle Context, recs []Recommendation) (kept []Recommendation, dropped []DroppedRecommendation) {
	known := flattenKnownValues(bundle)
	for _, r := range recs {
		if bad, ok := firstUngroundedFact(known, r.Evidence); ok {
			dropped = append(dropped, DroppedRecommendation{Recommendation: r, Reason: "evidence fact not found in input: " + bad})
			slog.Warn("ai: dropped ungrounded recommendation", "title", r.Title, "fact", bad)
			continue
		}
		kept = append(kept, r)
	}
	return kept, dropped
}

// knownValues holds every leaf value pulled out of a Context: numbers (as
// float64) for tolerant numeric comparison, and strings (tool/model/MCP
// server names, category/severity enums, ids, insight text) for
// case-insensitive comparison.
type knownValues struct {
	numbers []float64
	strings []string
}

func (k *knownValues) addNumber(n float64) {
	k.numbers = append(k.numbers, n)
}

func (k *knownValues) addOptNumber(n *float64) {
	if n != nil {
		k.numbers = append(k.numbers, *n)
	}
}

func (k *knownValues) addInt(n int64) {
	k.numbers = append(k.numbers, float64(n))
}

func (k *knownValues) addOptInt(n *int64) {
	if n != nil {
		k.numbers = append(k.numbers, float64(*n))
	}
}

func (k *knownValues) addString(s string) {
	if s != "" {
		k.strings = append(k.strings, strings.ToLower(s))
	}
}

func (k *knownValues) addOptString(s *string) {
	if s != nil {
		k.addString(*s)
	}
}

// flattenKnownValues walks bundle's typed fields directly (its shape is
// fully known, so no reflection is needed) collecting every numeric leaf
// (costs, tokens, counts, ratios, ms) and every string/name leaf.
// Rollups' totals are included too, so "total wasted spend of $X" grounds
// even though no single row carries the total.
func flattenKnownValues(bundle Context) knownValues {
	var k knownValues

	for _, f := range bundle.Insights {
		k.addString(f.Category)
		k.addString(f.Severity)
		k.addString(f.Title)
		k.addString(f.Detail)
	}
	for _, f := range bundle.TopPrompts {
		k.addString(f.PromptID)
		k.addOptInt(f.PromptLength)
		k.addNumber(f.TotalCostUSD)
		k.addInt(f.TotalTokens)
		k.addInt(f.LLMTurnCount)
		k.addOptNumber(f.CacheHitRatio)
		k.addInt(f.ToolCallCount)
		k.addInt(f.RejectedCount)
		k.addInt(f.ToolErrorCount)
		k.addInt(f.BlockedOnUserMs)
	}
	for _, f := range bundle.Waste {
		k.addString(f.SessionID)
		k.addString(f.Day)
		k.addNumber(f.WastedCostUSD)
		k.addInt(f.RejectedToolCount)
	}
	for _, f := range bundle.Tools {
		k.addString(f.ToolName)
		k.addOptString(f.MCPServerName)
		k.addOptString(f.MCPToolName)
		k.addInt(f.TotalCalls)
		k.addInt(f.UserTemporaryCount)
		k.addInt(f.UserPermanentCount)
		k.addInt(f.TotalBlockedOnUserMs)
		k.addInt(f.SuccessCount)
		k.addInt(f.ErrorCount)
		k.addOptNumber(f.AvgDurationMs)
	}
	for _, f := range bundle.MCP {
		k.addString(f.ServerName)
		k.addInt(f.ConnectionCount)
		k.addInt(f.ToolCallCount)
		k.addInt(f.DistinctToolsCalled)
	}
	for _, f := range bundle.Sessions {
		k.addString(f.SessionID)
		k.addInt(f.DurationMs)
		k.addNumber(f.TotalCostUSD)
		k.addNumber(f.TotalTokens)
		k.addInt(f.ToolCallCount)
		k.addInt(f.CompactionCount)
	}
	for _, f := range bundle.Models {
		k.addString(f.Model)
		k.addOptString(f.Effort)
		k.addOptString(f.Speed)
		k.addInt(f.TotalRequests)
		k.addOptNumber(f.ErrorRate)
		k.addOptNumber(f.P50TTFTMs)
		k.addOptNumber(f.P95TTFTMs)
		k.addOptNumber(f.P50DurationMs)
		k.addOptNumber(f.P95DurationMs)
	}
	for _, f := range bundle.Payload {
		k.addString(f.ToolName)
		k.addOptString(f.MCPServerName)
		k.addInt(f.CallCount)
		k.addInt(f.TotalInputBytes)
		k.addInt(f.TotalResultBytes)
		k.addOptNumber(f.AvgResultBytes)
	}
	for _, f := range bundle.Retries {
		k.addString(f.SessionID)
		k.addString(f.Day)
		k.addInt(f.RetriedRequests)
		k.addNumber(f.RetriedCostUSD)
	}

	k.addNumber(bundle.Rollups.TotalWastedSpendUSD)
	k.addInt(bundle.Rollups.TotalRejectedToolCalls)
	k.addNumber(bundle.Rollups.TotalRetriedCostUSD)
	k.addInt(bundle.Rollups.SessionCountInRange)

	return k
}

// firstUngroundedFact returns the first evidence fact (its Value) that
// can't be matched against known, or ("", false) if all facts ground.
func firstUngroundedFact(known knownValues, facts []EvidenceFact) (string, bool) {
	for _, f := range facts {
		if n, ok := parseNumeric(f.Value); ok {
			if !numberGrounds(known.numbers, n) {
				return f.Value, true
			}
			continue
		}
		if !stringGrounds(known.strings, f.Value) {
			return f.Value, true
		}
	}
	return "", false
}

// parseNumeric strips common formatting ($, commas, %, and unit suffixes
// like "ms"/"s"/"tokens") and tries to parse the remainder as a float64.
func parseNumeric(v string) (float64, bool) {
	s := strings.TrimSpace(v)
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "%")
	s = strings.ReplaceAll(s, ",", "")
	for _, suffix := range []string{" tokens", " ms", " s", " sec", " secs", " seconds", " calls", " requests"} {
		s = strings.TrimSuffix(s, suffix)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// numberGrounds reports whether n matches some k in known within
// tolerance: tight enough that a fabricated number won't accidentally
// match, loose enough to tolerate rounding/currency-formatting differences
// in how the model renders a number it copied correctly.
func numberGrounds(known []float64, n float64) bool {
	for _, k := range known {
		tolerance := math.Max(0.01, math.Abs(k)*0.005)
		if math.Abs(n-k) <= tolerance {
			return true
		}
	}
	return false
}

// stringGrounds reports whether v (case-folded) exactly equals or is a
// substring of some entry in known — covers exact tool/model/server names
// as well as short model-composed phrases that embed a known name.
func stringGrounds(known []string, v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return true // an empty evidence value carries no claim to ground
	}
	for _, k := range known {
		if k == v || strings.Contains(k, v) || strings.Contains(v, k) {
			return true
		}
	}
	return false
}
