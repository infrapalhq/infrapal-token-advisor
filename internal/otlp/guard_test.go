package otlp

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// collector returns the full handler, with every check a real request meets.
func collector(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewServer("127.0.0.1:0", ingest.NewWriter(db), ingest.NewMetricsWriter(db), ingest.NewSpansWriter(db)).http.Handler
}

func post(h http.Handler, body io.Reader, edit func(*http.Request)) int {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4318/v1/logs", body)
	req.Header.Set("Content-Type", "application/x-protobuf")
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestCollector_AcceptsAnExporter(t *testing.T) {
	payload := buildEnvelope(t, &logspb.ResourceLogs{})
	if got := post(collector(t), bytes.NewReader(payload), nil); got != http.StatusOK {
		t.Errorf("status %d, want 200", got)
	}
}

// A web page can send a simple POST to localhost without asking. These are the
// ways it would try.
func TestCollector_RefusesWebPages(t *testing.T) {
	h := collector(t)
	payload := buildEnvelope(t, &logspb.ResourceLogs{})
	cases := map[string]func(*http.Request){
		"plain text form post":     func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		"no content type":          func(r *http.Request) { r.Header.Del("Content-Type") },
		"from another site":        func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"from another origin":      func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"through a rebound domain": func(r *http.Request) { r.Host = "evil.example:4318" },
	}
	for name, edit := range cases {
		if got := post(h, bytes.NewReader(payload), edit); got == http.StatusOK {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCollector_RefusesOversizedPayloads(t *testing.T) {
	h := collector(t)
	if got := post(h, bytes.NewReader(make([]byte, maxBodyBytes+1)), nil); got != http.StatusRequestEntityTooLarge {
		t.Errorf("large body: status %d, want 413", got)
	}

	// A gzip bomb: a small request that would decompress past the limit.
	var bomb bytes.Buffer
	gz := gzip.NewWriter(&bomb)
	if _, err := gz.Write(make([]byte, maxDecompressedBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if bomb.Len() > maxBodyBytes {
		t.Fatalf("the bomb itself is %d bytes, so this wouldn't test decompression", bomb.Len())
	}
	got := post(h, &bomb, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") })
	if got != http.StatusRequestEntityTooLarge {
		t.Errorf("gzip bomb: status %d, want 413", got)
	}
}
