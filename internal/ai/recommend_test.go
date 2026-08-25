package ai

import (
	"context"
	"testing"
)

// fakeRequester lets Generate be tested without a real network call.
type fakeRequester struct {
	calls int
	recs  []Recommendation
}

func (f *fakeRequester) RequestRecommendations(ctx context.Context, bundle Context) ([]Recommendation, bool, error) {
	f.calls++
	return f.recs, false, nil
}

func TestGenerate_CacheMissThenHit(t *testing.T) {
	db := seedDB(t)
	fake := &fakeRequester{recs: []Recommendation{
		{Category: "waste", Severity: "info", Title: "Reduce retries", Detail: "d",
			Evidence: []EvidenceFact{{Label: "cost", Value: "0.1"}}},
	}}

	result1, err := Generate(context.Background(), db, fake, "", 0, false, nil, false)
	if err != nil {
		t.Fatalf("Generate (miss): %v", err)
	}
	if result1.FromCache {
		t.Errorf("first call: FromCache = true, want false")
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 API call after first Generate, got %d", fake.calls)
	}
	if len(result1.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %+v", result1.Recommendations)
	}

	result2, err := Generate(context.Background(), db, fake, "", 0, false, nil, false)
	if err != nil {
		t.Fatalf("Generate (hit): %v", err)
	}
	if !result2.FromCache {
		t.Errorf("second call: FromCache = false, want true")
	}
	if fake.calls != 1 {
		t.Errorf("expected the fake to still have been called only once (cache hit), got %d calls", fake.calls)
	}
	if result2.BatchID != result1.BatchID {
		t.Errorf("cache hit returned a different batch: %q vs %q", result2.BatchID, result1.BatchID)
	}
}

func TestGenerate_ForceAlwaysCallsRequester(t *testing.T) {
	db := seedDB(t)
	fake := &fakeRequester{recs: []Recommendation{
		{Category: "waste", Severity: "info", Title: "t", Detail: "d",
			Evidence: []EvidenceFact{{Label: "cost", Value: "0.1"}}},
	}}

	if _, err := Generate(context.Background(), db, fake, "", 0, false, nil, false); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := Generate(context.Background(), db, fake, "", 0, false, nil, true); err != nil {
		t.Fatalf("Generate (force): %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("expected force=true to always call the requester, got %d calls", fake.calls)
	}
}

func TestGenerate_UngroundedRecommendationDroppedNotPersisted(t *testing.T) {
	db := seedDB(t)
	fake := &fakeRequester{recs: []Recommendation{
		{Category: "waste", Severity: "info", Title: "fabricated", Detail: "d",
			Evidence: []EvidenceFact{{Label: "cost", Value: "999999.99"}}},
	}}

	result, err := Generate(context.Background(), db, fake, "", 0, false, nil, false)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(result.Recommendations) != 0 {
		t.Errorf("expected the ungrounded recommendation to be dropped, got %+v", result.Recommendations)
	}
	if result.DroppedCount != 1 {
		t.Errorf("DroppedCount = %d, want 1", result.DroppedCount)
	}

	cached, err := GetCached(context.Background(), db, "")
	if err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if len(cached.Recommendations) != 0 {
		t.Errorf("expected nothing persisted for a fully-dropped batch, got %+v", cached.Recommendations)
	}
}

func TestGetCached_EmptyWhenNeverGenerated(t *testing.T) {
	db := seedDB(t)
	result, err := GetCached(context.Background(), db, "30d")
	if err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if len(result.Recommendations) != 0 {
		t.Errorf("expected empty recommendations, got %+v", result.Recommendations)
	}
	if result.FromCache {
		t.Errorf("FromCache = true for a never-generated range, want false")
	}
}
