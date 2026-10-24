package otlp

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func TestHandleLogs_EndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ccotel.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	writer := ingest.NewWriter(db)
	ctx, cancel := context.WithCancel(context.Background())
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(ctx) }()

	record := &logspb.LogRecord{
		TimeUnixNano: 1_700_000_000_000_000_000,
		Attributes: []*commonpb.KeyValue{
			{Key: "event.name", Value: strVal("claude_code.user_prompt")},
			{Key: "prompt.id", Value: strVal("p-e2e")},
		},
	}
	rl := &logspb.ResourceLogs{ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{record}}}}
	payload := buildEnvelope(t, rl)

	srv := NewServer("127.0.0.1:0", writer, ingest.NewMetricsWriter(db), ingest.NewSpansWriter(db))
	ts := httptest.NewServer(http.HandlerFunc(srv.handleLogs))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/x-protobuf", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /v1/logs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Give the async writer a moment, then force a flush via shutdown.
	cancel()
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("writer.Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not flush in time")
	}

	var promptID string
	var eventName string
	err = db.QueryRow(`SELECT prompt_id, event_name FROM events_raw WHERE prompt_id = ?`, "p-e2e").Scan(&promptID, &eventName)
	if err != nil {
		if err == sql.ErrNoRows {
			t.Fatal("no row landed in events_raw for prompt p-e2e")
		}
		t.Fatalf("query events_raw: %v", err)
	}
	if eventName != "claude_code.user_prompt" {
		t.Errorf("event_name = %q, want claude_code.user_prompt", eventName)
	}
}
