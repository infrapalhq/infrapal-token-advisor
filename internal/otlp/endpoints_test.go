package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// liveCollector runs the full collector with its writers, and returns a way to
// stop it once everything sent has been written.
func liveCollector(t *testing.T) (http.Handler, *sql.DB, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	logs, metrics, spans := ingest.NewWriter(db), ingest.NewMetricsWriter(db), ingest.NewSpansWriter(db)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 3)
	go func() { done <- logs.Run(ctx) }()
	go func() { done <- metrics.Run(ctx) }()
	go func() { done <- spans.Run(ctx) }()
	stop := func() {
		cancel()
		for range 3 {
			if err := <-done; err != nil {
				t.Errorf("writer: %v", err)
			}
		}
	}
	return NewServer("127.0.0.1:0", logs, metrics, spans).http.Handler, db, stop
}

func send(h http.Handler, method, path string, body []byte, gzipped bool) int {
	if gzipped {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(body)
		_ = gz.Close()
		body = buf.Bytes()
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:4318"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	if gzipped {
		req.Header.Set("Content-Encoding", "gzip")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestCollector_StoresMetricsAndSpans(t *testing.T) {
	h, db, stop := liveCollector(t)
	session := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "session.id", Value: strVal("sess-1")}}}
	metrics := buildMetricsEnvelope(t, &metricspb.ResourceMetrics{
		Resource: session,
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
			Name: "claude_code.token.usage",
			Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{{
				TimeUnixNano: 1_700_000_000_000_000_000,
				Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 123},
			}}}},
		}}}},
	})
	traces := buildTracesEnvelope(t, &tracepb.ResourceSpans{
		Resource: session,
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{
			TraceId: []byte{1}, SpanId: []byte{2}, Name: "claude_code.tool",
			StartTimeUnixNano: 1_000, EndTimeUnixNano: 2_000,
			Attributes: []*commonpb.KeyValue{{Key: "tool_use_id", Value: strVal("tu-1")}},
		}}}},
	})
	if got := send(h, http.MethodPost, "/v1/metrics", metrics, false); got != http.StatusOK {
		t.Errorf("metrics: status %d", got)
	}
	if got := send(h, http.MethodPost, "/v1/traces", traces, true); got != http.StatusOK {
		t.Errorf("gzipped traces: status %d", got)
	}
	stop()

	var value float64
	if err := db.QueryRow(`SELECT value FROM metric_points WHERE name = 'claude_code.token.usage'`).Scan(&value); err != nil || value != 123 {
		t.Errorf("metric = %v, %v", value, err)
	}
	var spans int
	if err := db.QueryRow(`SELECT COUNT(*) FROM spans WHERE span_id = '02'`).Scan(&spans); err != nil || spans != 1 {
		t.Errorf("spans = %d, %v", spans, err)
	}
}

func TestCollector_RefusesBadRequests(t *testing.T) {
	h, _, stop := liveCollector(t)
	defer stop()
	for _, path := range []string{"/v1/logs", "/v1/metrics", "/v1/traces"} {
		if got := send(h, http.MethodGet, path, nil, false); got != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status %d, want 405", path, got)
		}
		if got := send(h, http.MethodPost, path, []byte{0xff, 0xff, 0xff}, false); got != http.StatusBadRequest {
			t.Errorf("garbage to %s: status %d, want 400", path, got)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4318/v1/logs", bytes.NewReader([]byte("not gzip")))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("broken gzip: status %d, want 400", rec.Code)
	}
}
