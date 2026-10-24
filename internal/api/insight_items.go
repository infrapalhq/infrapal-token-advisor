package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"
)

// insightItem is one aggregated problem: every occurrence of one kind for one
// subject (a file, a command, a pattern, a server), across the window. This
// is what the Fixes page and the Overview's "Fix first" list show.
type insightItem struct {
	Key         string   `json:"key"` // kind|subject, the id used by POST /api/fixes/{id}/state
	Kind        string   `json:"kind"`
	Area        string   `json:"area"`
	Subject     string   `json:"subject"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Severity    string   `json:"severity"` // full | half
	Conf        string   `json:"conf"`     // Measured | Likely | Estimated
	Occurrences int      `json:"occurrences"`
	Sessions    int      `json:"sessions"`
	Tokens      *int64   `json:"tokens,omitempty"`
	USD         *float64 `json:"usd,omitempty"`
	Minutes     *float64 `json:"minutes,omitempty"`
	Fix         string   `json:"fix,omitempty"`
	FixLang     string   `json:"fix_lang,omitempty"`
	Note        string   `json:"note,omitempty"`
	Score       float64  `json:"score"`
	LastTs      int64    `json:"last_ts"`
	Marked      bool     `json:"marked"`
	MarkedAt    int64    `json:"marked_at,omitempty"`

	sessionSet map[string]struct{}
	evidence   []string
}

// confWeight scales how much a confidence level counts in the priority score.
var confWeight = map[string]float64{"Measured": 1.0, "Likely": 0.7, "Estimated": 0.8}

// aggregateItems groups occurrences into items and scores them. rate prices
// token-only occurrences in USD using this install's own blended rate.
func aggregateItems(occ []occurrence, rate float64) []*insightItem {
	byKey := map[string]*insightItem{}
	var order []string
	for _, o := range occ {
		key := o.Kind + "|" + o.Subject
		it, ok := byKey[key]
		if !ok {
			it = &insightItem{
				Key:        key,
				Kind:       o.Kind,
				Area:       o.Area,
				Subject:    o.Subject,
				Title:      o.Title,
				Detail:     o.Detail,
				Fix:        o.Fix,
				FixLang:    o.FixLang,
				Note:       o.Note,
				Severity:   o.Severity,
				Conf:       o.Conf,
				sessionSet: map[string]struct{}{},
			}
			byKey[key] = it
			order = append(order, key)
		}
		it.Occurrences++
		it.sessionSet[o.SessionID] = struct{}{}
		if o.Severity == "full" {
			it.Severity = "full"
		}
		if o.Conf == "Likely" && it.Conf == "Measured" {
			it.Conf = "Likely"
		}
		if o.Ts > it.LastTs {
			it.LastTs = o.Ts
		}
		if o.Tokens != nil {
			if it.Tokens == nil {
				var z int64
				it.Tokens = &z
			}
			*it.Tokens += *o.Tokens
		}
		if o.USD != nil {
			if it.USD == nil {
				var z float64
				it.USD = &z
			}
			*it.USD += *o.USD
		}
		if o.Minutes != nil {
			if it.Minutes == nil {
				var z float64
				it.Minutes = &z
			}
			*it.Minutes += *o.Minutes
		}
		if len(it.evidence) < 50 {
			it.evidence = append(it.evidence, o.Evidence...)
		}
	}

	out := make([]*insightItem, 0, len(order))
	for _, key := range order {
		it := byKey[key]
		it.Sessions = len(it.sessionSet)
		if it.Tokens != nil && it.USD == nil && rate > 0 {
			usd := float64(*it.Tokens) * rate
			it.USD = &usd
		}
		if it.Occurrences > 1 {
			it.Title = it.Title + " · " + plural3(it.Occurrences, it.Sessions)
		}
		it.Score = priorityScore(it)
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].LastTs > out[j].LastTs
	})
	return out
}

// priorityScore is SPEC §5: (usd + minutes × 0.10) × confidence × log2(1+n).
// The 0.10 $/min weight only ranks items; it is never shown as money.
func priorityScore(it *insightItem) float64 {
	var usd, minutes float64
	if it.USD != nil {
		usd = *it.USD
	}
	if it.Minutes != nil {
		minutes = *it.Minutes
	}
	w := confWeight[it.Conf]
	if w == 0 {
		w = 0.5
	}
	return (usd + minutes*0.10) * w * math.Log2(1+float64(it.Occurrences))
}

func plural3(occurrences, sessions int) string {
	s := "s"
	if sessions == 1 {
		s = ""
	}
	return strconv.Itoa(occurrences) + " occurrences in " + strconv.Itoa(sessions) + " session" + s
}

// markedState is the latest fix_marks row per item key.
type markedState struct {
	state    string
	markedAt int64
}

// loadMarks returns the latest fix-mark state per "kind|subject".
func loadMarks(ctx context.Context, db *sql.DB) (map[string]markedState, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT finding_id, subject, state, marked_at FROM fix_marks
		WHERE id IN (SELECT MAX(id) FROM fix_marks GROUP BY finding_id, subject)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]markedState{}
	for rows.Next() {
		var kind, subject, state string
		var at int64
		if err := rows.Scan(&kind, &subject, &state, &at); err != nil {
			return nil, err
		}
		out[kind+"|"+subject] = markedState{state: state, markedAt: at}
	}
	return out, rows.Err()
}

// applyMarks sets Marked/MarkedAt from the latest mark per item.
func applyMarks(items []*insightItem, marks map[string]markedState) {
	for _, it := range items {
		if m, ok := marks[it.Key]; ok && m.state == "fixed" {
			it.Marked = true
			it.MarkedAt = m.markedAt
		}
	}
}

// fixBaseline is the metric snapshot stored when a fix is marked, so the
// Fixes page can later compare before and after.
type fixBaseline struct {
	Occurrences int      `json:"occurrences"`
	Sessions    int      `json:"sessions"`
	Tokens      *int64   `json:"tokens,omitempty"`
	USD         *float64 `json:"usd,omitempty"`
	Minutes     *float64 `json:"minutes,omitempty"`
}

// markDuration is the minimum measuring window after a mark (SPEC §8).
const markDuration = 7 * 24 * time.Hour

// recordMark appends a fixed/reverted row for item key, capturing the baseline
// from the item's current totals.
func recordMark(ctx context.Context, db *sql.DB, it *insightItem, state string, now time.Time) error {
	base, err := json.Marshal(fixBaseline{
		Occurrences: it.Occurrences,
		Sessions:    it.Sessions,
		Tokens:      it.Tokens,
		USD:         it.USD,
		Minutes:     it.Minutes,
	})
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO fix_marks (finding_id, subject, marked_at, baseline_json, measure_until, state)
		VALUES (?, ?, ?, ?, ?, ?)`,
		it.Kind, it.Subject, now.UnixMilli(), string(base), now.Add(markDuration).UnixMilli(), state,
	)
	return err
}
