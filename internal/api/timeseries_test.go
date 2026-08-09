package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

type tsPoint struct {
	Bucket string  `json:"bucket"`
	Value  float64 `json:"value"`
}

func TestHandleTimeseries_Cost(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	// Fixture's metric_points are seeded at ts=1000 (near the Unix epoch), so
	// a huge "since" is needed to pull them into the window.
	resp, err := http.Get(ts.URL + "/api/timeseries?metric=cost&bucket=day&since=36500d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var points []tsPoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 bucket, got %d: %+v", len(points), points)
	}
	if points[0].Value != 0.1 {
		t.Errorf("value = %v, want 0.1", points[0].Value)
	}
}

func TestHandleTimeseries_Sessions(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/timeseries?metric=sessions&bucket=day&since=36500d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var points []tsPoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(points) != 1 || points[0].Value != 1 {
		t.Fatalf("points = %+v, want one bucket with value 1", points)
	}
}

func TestHandleTimeseries_DefaultSinceExcludesOldFixture(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	// No ?since= -> defaults to 7d, which excludes the fixture's ts=1000
	// points. Confirms the default doesn't error, just returns empty.
	resp, err := http.Get(ts.URL + "/api/timeseries?metric=cost")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var points []tsPoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("expected 0 buckets within default 7d window, got %d", len(points))
	}
}

func TestHandleTimeseries_UnknownMetric(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/timeseries?metric=not_a_real_metric")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTimeseries_UnknownBucket(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/timeseries?metric=cost&bucket=fortnight")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleTimeseries_InvalidSince(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/timeseries?metric=cost&since=notaduration")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleTimeseries_TypeFilteredMetric exercises an alias that reads a
// json_extract(attrs, '$.type') filter (lines_added), not just a bare metric
// name lookup.
func TestHandleTimeseries_TypeFilteredMetric(t *testing.T) {
	db := seedDB(t)
	if _, err := db.Exec(`
		INSERT INTO metric_points (ts, name, value, session_id, attrs)
		VALUES (1000, 'claude_code.lines_of_code.count', 42, 's1', '{"type":"added"}')
	`); err != nil {
		t.Fatalf("seed lines_added: %v", err)
	}
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/timeseries?metric=lines_added&bucket=day&since=36500d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var points []tsPoint
	if err := json.NewDecoder(resp.Body).Decode(&points); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(points) != 1 || points[0].Value != 42 {
		t.Fatalf("points = %+v, want one bucket with value 42", points)
	}
}
