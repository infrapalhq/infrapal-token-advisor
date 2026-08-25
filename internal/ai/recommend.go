package ai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// StoredRecommendation is one persisted, already-grounded recommendation.
type StoredRecommendation struct {
	ID          int64          `json:"id"`
	BatchID     string         `json:"batch_id"`
	GeneratedAt int64          `json:"generated_at"`
	Category    string         `json:"category"`
	Severity    string         `json:"severity"`
	Title       string         `json:"title"`
	Detail      string         `json:"detail"`
	Evidence    []EvidenceFact `json:"evidence"`
}

// GenerateResult is the response to both GET and POST
// /api/ai/recommendations.
type GenerateResult struct {
	BatchID         string                 `json:"batch_id"`
	GeneratedAt     int64                  `json:"generated_at"`
	FromCache       bool                   `json:"from_cache"`
	Recommendations []StoredRecommendation `json:"recommendations"`
	DroppedCount    int                    `json:"dropped_count"` // model proposed but failed grounding
}

// Generate implements one POST /api/ai/recommendations call: build the
// context, check the cache (unless force), else call req, validate,
// persist, and return. req is an interface (satisfied by *Client) so
// callers/tests can inject a fake instead of hitting the real API.
func Generate(ctx context.Context, db *sql.DB, req Requester, sinceRange string, cutoffMs int64, haveCutoff bool, insights []InsightFact, force bool) (GenerateResult, error) {
	bundle, err := BuildContext(ctx, db, sinceRange, cutoffMs, haveCutoff, insights)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("build context: %w", err)
	}
	hash, err := bundle.Hash()
	if err != nil {
		return GenerateResult{}, err
	}

	if !force {
		if cached, ok, err := loadCachedBatch(ctx, db, sinceRange, hash); err != nil {
			return GenerateResult{}, err
		} else if ok {
			cached.FromCache = true
			return cached, nil
		}
	}

	recs, insufficientData, err := req.RequestRecommendations(ctx, bundle)
	if err != nil {
		return GenerateResult{}, err
	}
	var kept []Recommendation
	var dropped []DroppedRecommendation
	if !insufficientData {
		kept, dropped = ValidateEvidence(bundle, recs)
	}

	batchID, err := newBatchID()
	if err != nil {
		return GenerateResult{}, err
	}
	generatedAt := time.Now().UnixMilli()
	if err := persistBatch(ctx, db, batchID, generatedAt, sinceRange, hash, HaikuModel, kept); err != nil {
		return GenerateResult{}, err
	}

	return GenerateResult{
		BatchID:         batchID,
		GeneratedAt:     generatedAt,
		FromCache:       false,
		Recommendations: toStored(batchID, generatedAt, kept),
		DroppedCount:    len(dropped),
	}, nil
}

// GetCached implements GET /api/ai/recommendations?since= — read-only,
// never calls Anthropic. Returns a zero-value result (empty
// Recommendations) if Generate has never run for sinceRange.
func GetCached(ctx context.Context, db *sql.DB, sinceRange string) (GenerateResult, error) {
	var batchID string
	var generatedAt int64
	err := db.QueryRowContext(ctx,
		`SELECT batch_id, generated_at FROM ai_recommendations
			WHERE since_range = ? ORDER BY generated_at DESC LIMIT 1`, sinceRange,
	).Scan(&batchID, &generatedAt)
	if err == sql.ErrNoRows {
		return GenerateResult{Recommendations: []StoredRecommendation{}}, nil
	}
	if err != nil {
		return GenerateResult{}, fmt.Errorf("load latest batch: %w", err)
	}

	recs, err := loadBatchRows(ctx, db, batchID)
	if err != nil {
		return GenerateResult{}, err
	}
	return GenerateResult{
		BatchID:         batchID,
		GeneratedAt:     generatedAt,
		FromCache:       true,
		Recommendations: recs,
	}, nil
}

// loadCachedBatch looks up the most recent batch matching (sinceRange,
// hash) exactly — an unchanged-data cache hit for the "Generate" click.
func loadCachedBatch(ctx context.Context, db *sql.DB, sinceRange, hash string) (GenerateResult, bool, error) {
	var batchID string
	var generatedAt int64
	err := db.QueryRowContext(ctx,
		`SELECT batch_id, generated_at FROM ai_recommendations
			WHERE since_range = ? AND input_hash = ? ORDER BY generated_at DESC LIMIT 1`, sinceRange, hash,
	).Scan(&batchID, &generatedAt)
	if err == sql.ErrNoRows {
		return GenerateResult{}, false, nil
	}
	if err != nil {
		return GenerateResult{}, false, fmt.Errorf("load cached batch: %w", err)
	}

	recs, err := loadBatchRows(ctx, db, batchID)
	if err != nil {
		return GenerateResult{}, false, err
	}
	return GenerateResult{
		BatchID:         batchID,
		GeneratedAt:     generatedAt,
		Recommendations: recs,
	}, true, nil
}

func loadBatchRows(ctx context.Context, db *sql.DB, batchID string) ([]StoredRecommendation, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, batch_id, generated_at, category, severity, title, detail, evidence_json
			FROM ai_recommendations WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, fmt.Errorf("load batch rows: %w", err)
	}
	defer rows.Close()

	out := []StoredRecommendation{}
	for rows.Next() {
		var r StoredRecommendation
		var evidenceJSON string
		if err := rows.Scan(&r.ID, &r.BatchID, &r.GeneratedAt, &r.Category, &r.Severity, &r.Title, &r.Detail, &evidenceJSON); err != nil {
			return nil, fmt.Errorf("scan batch row: %w", err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &r.Evidence); err != nil {
			return nil, fmt.Errorf("unmarshal evidence: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func persistBatch(ctx context.Context, db *sql.DB, batchID string, generatedAt int64, sinceRange, hash, model string, recs []Recommendation) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin persist batch: %w", err)
	}
	defer tx.Rollback()

	for _, r := range recs {
		evidenceJSON, err := json.Marshal(r.Evidence)
		if err != nil {
			return fmt.Errorf("marshal evidence: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ai_recommendations
				(batch_id, generated_at, since_range, input_hash, model, category, severity, title, detail, evidence_json)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			batchID, generatedAt, sinceRange, hash, model, r.Category, r.Severity, r.Title, r.Detail, string(evidenceJSON),
		); err != nil {
			return fmt.Errorf("insert recommendation: %w", err)
		}
	}
	return tx.Commit()
}

func toStored(batchID string, generatedAt int64, recs []Recommendation) []StoredRecommendation {
	out := make([]StoredRecommendation, len(recs))
	for i, r := range recs {
		out[i] = StoredRecommendation{
			BatchID:     batchID,
			GeneratedAt: generatedAt,
			Category:    r.Category,
			Severity:    r.Severity,
			Title:       r.Title,
			Detail:      r.Detail,
			Evidence:    r.Evidence,
		}
	}
	return out
}

// newBatchID returns a random 16-byte hex id via crypto/rand — no new
// dependency needed for this.
func newBatchID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate batch id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
