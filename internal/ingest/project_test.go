package ingest

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mkEvent(name, sessionID, promptID string, attrs map[string]any) RawEvent {
	return RawEvent{
		Ts:        1_700_000_000_000,
		SessionID: sessionID,
		PromptID:  promptID,
		EventName: name,
		Attrs:     attrs,
		Body:      "{}",
	}
}

// --- tool_calls merge across tool_decision / tool_result, either order ---

func TestToolCallMerge_DecisionThenResult(t *testing.T) {
	db := openTestDB(t)

	decision := mkEvent("claude_code.tool_decision", "sess-1", "prompt-1", map[string]any{
		"tool_use_id": "tu-1",
		"tool_name":   "Bash",
		"tool_source": "assistant",
		"decision":    "accept",
		"source":      "user",
	})
	if err := writeLogsBatch(db, []RawEvent{decision}); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	result := mkEvent("claude_code.tool_result", "sess-1", "prompt-1", map[string]any{
		"tool_use_id": "tu-1",
		"tool_name":   "Bash",
		"duration_ms": int64(250),
		"success":     true,
	})
	if err := writeLogsBatch(db, []RawEvent{result}); err != nil {
		t.Fatalf("write result: %v", err)
	}

	assertToolCallMerged(t, db)
}

func TestToolCallMerge_ResultThenDecision(t *testing.T) {
	db := openTestDB(t)

	result := mkEvent("claude_code.tool_result", "sess-1", "prompt-1", map[string]any{
		"tool_use_id": "tu-1",
		"tool_name":   "Bash",
		"duration_ms": int64(250),
		"success":     true,
	})
	if err := writeLogsBatch(db, []RawEvent{result}); err != nil {
		t.Fatalf("write result: %v", err)
	}

	decision := mkEvent("claude_code.tool_decision", "sess-1", "prompt-1", map[string]any{
		"tool_use_id": "tu-1",
		"tool_name":   "Bash",
		"tool_source": "assistant",
		"decision":    "accept",
		"source":      "user",
	})
	if err := writeLogsBatch(db, []RawEvent{decision}); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	assertToolCallMerged(t, db)
}

func assertToolCallMerged(t *testing.T, db *sql.DB) {
	t.Helper()
	var toolSource string
	var durationMs sql.NullInt64
	var success sql.NullInt64
	err := db.QueryRow(`SELECT tool_source, duration_ms, success FROM tool_calls WHERE tool_use_id = ?`, "tu-1").
		Scan(&toolSource, &durationMs, &success)
	if err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	// tool_result's own "decision" field ("accept", hardcoded — see
	// upsertToolCallFromResult) must never stomp tool_decision's
	// tool_source, and vice versa regardless of write order.
	if toolSource != "assistant" {
		t.Errorf("tool_source = %q, want assistant (from tool_decision, must survive either write order)", toolSource)
	}
	if !durationMs.Valid || durationMs.Int64 != 250 {
		t.Errorf("duration_ms = %v, want 250 (from tool_result)", durationMs)
	}
	if !success.Valid || success.Int64 != 1 {
		t.Errorf("success = %v, want 1", success)
	}
}

// --- interactions <-> claude_code.interaction span correlation, either order ---

func TestInteractionSpanCorrelation_EventThenSpan(t *testing.T) {
	db := openTestDB(t)

	prompt := mkEvent("claude_code.user_prompt", "sess-1", "prompt-1", map[string]any{
		"prompt_length": int64(10),
	})
	prompt.TraceID = "trace-1"
	prompt.SpanID = "span-1"
	if err := writeLogsBatch(db, []RawEvent{prompt}); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	sp := Span{
		TraceID: "trace-1",
		SpanID:  "span-1",
		Name:    "claude_code.interaction",
		StartNs: 1_000_000_000,
		EndNs:   1_500_000_000,
		Attrs:   map[string]any{},
	}
	if err := writeSpansBatch(db, []Span{sp}); err != nil {
		t.Fatalf("write span: %v", err)
	}

	assertInteractionDuration(t, db, "prompt-1", 500)
	assertNoPendingInteractionSpans(t, db)
}

func TestInteractionSpanCorrelation_SpanThenEvent(t *testing.T) {
	db := openTestDB(t)

	sp := Span{
		TraceID: "trace-1",
		SpanID:  "span-1",
		Name:    "claude_code.interaction",
		StartNs: 1_000_000_000,
		EndNs:   1_500_000_000,
		Attrs:   map[string]any{},
	}
	if err := writeSpansBatch(db, []Span{sp}); err != nil {
		t.Fatalf("write span: %v", err)
	}

	// Staged, waiting for an interactions row to claim it.
	var pendingCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_interaction_spans`).Scan(&pendingCount); err != nil {
		t.Fatalf("query pending_interaction_spans: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected 1 pending interaction span, got %d", pendingCount)
	}

	prompt := mkEvent("claude_code.user_prompt", "sess-1", "prompt-1", map[string]any{
		"prompt_length": int64(10),
	})
	prompt.TraceID = "trace-1"
	prompt.SpanID = "span-1"
	if err := writeLogsBatch(db, []RawEvent{prompt}); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	assertInteractionDuration(t, db, "prompt-1", 500)
	assertNoPendingInteractionSpans(t, db)
}

func assertInteractionDuration(t *testing.T, db *sql.DB, promptID string, want int64) {
	t.Helper()
	var durationMs sql.NullInt64
	if err := db.QueryRow(`SELECT duration_ms FROM interactions WHERE prompt_id = ?`, promptID).Scan(&durationMs); err != nil {
		t.Fatalf("query interactions: %v", err)
	}
	if !durationMs.Valid || durationMs.Int64 != want {
		t.Errorf("duration_ms = %v, want %d", durationMs, want)
	}
}

func assertNoPendingInteractionSpans(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_interaction_spans`).Scan(&count); err != nil {
		t.Fatalf("query pending_interaction_spans: %v", err)
	}
	if count != 0 {
		t.Errorf("expected pending_interaction_spans to be drained, found %d row(s)", count)
	}
}

// --- assistant_response dropped without message.uuid ---

func TestAssistantResponse_DroppedWithoutMessageUUID(t *testing.T) {
	db := openTestDB(t)

	e := mkEvent("claude_code.assistant_response", "sess-1", "prompt-1", map[string]any{
		"response": "hi there",
	})
	if err := writeLogsBatch(db, []RawEvent{e}); err != nil {
		t.Fatalf("write assistant_response: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM assistant_responses`).Scan(&count); err != nil {
		t.Fatalf("query assistant_responses: %v", err)
	}
	if count != 0 {
		t.Errorf("expected assistant_response without message.uuid to be dropped, found %d row(s)", count)
	}
}

func TestAssistantResponse_KeptWithMessageUUID(t *testing.T) {
	db := openTestDB(t)

	e := mkEvent("claude_code.assistant_response", "sess-1", "prompt-1", map[string]any{
		"message.uuid": "msg-1",
		"response":     "hi there",
	})
	if err := writeLogsBatch(db, []RawEvent{e}); err != nil {
		t.Fatalf("write assistant_response: %v", err)
	}

	var responseText string
	if err := db.QueryRow(`SELECT response_text FROM assistant_responses WHERE message_uuid = ?`, "msg-1").Scan(&responseText); err != nil {
		t.Fatalf("query assistant_responses: %v", err)
	}
	if responseText != "hi there" {
		t.Errorf("response_text = %q, want %q", responseText, "hi there")
	}
}

// --- claude_code.tool.blocked_on_user resolves tool_use_id via its parent span ---

func TestToolBlockedOnUserSpan_ResolvesToolUseIDFromParent(t *testing.T) {
	db := openTestDB(t)

	parent := Span{
		TraceID: "trace-1",
		SpanID:  "span-tool-1",
		Name:    "claude_code.tool",
		StartNs: 1_000_000_000,
		EndNs:   2_000_000_000,
		Attrs:   map[string]any{"tool_use_id": "tu-1", "tool_name": "Bash"},
	}
	if err := writeSpansBatch(db, []Span{parent}); err != nil {
		t.Fatalf("write parent span: %v", err)
	}

	child := Span{
		TraceID:      "trace-1",
		SpanID:       "span-blocked-1",
		ParentSpanID: "span-tool-1",
		Name:         "claude_code.tool.blocked_on_user",
		StartNs:      1_200_000_000,
		EndNs:        1_800_000_000,
		Attrs:        map[string]any{"duration_ms": int64(600), "decision": "accept", "source": "user"},
	}
	if err := writeSpansBatch(db, []Span{child}); err != nil {
		t.Fatalf("write child span: %v", err)
	}

	var blockedMs sql.NullInt64
	if err := db.QueryRow(`SELECT blocked_on_user_ms FROM tool_calls WHERE tool_use_id = ?`, "tu-1").Scan(&blockedMs); err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	if !blockedMs.Valid || blockedMs.Int64 != 600 {
		t.Errorf("blocked_on_user_ms = %v, want 600", blockedMs)
	}
}

func TestToolBlockedOnUserSpan_ParentNeverArrives_StagedWithoutError(t *testing.T) {
	db := openTestDB(t)

	child := Span{
		TraceID:      "trace-1",
		SpanID:       "span-blocked-1",
		ParentSpanID: "span-tool-missing",
		Name:         "claude_code.tool.blocked_on_user",
		StartNs:      1_200_000_000,
		EndNs:        1_800_000_000,
		Attrs:        map[string]any{"duration_ms": int64(600)},
	}
	if err := writeSpansBatch(db, []Span{child}); err != nil {
		t.Fatalf("write child span should not error even though parent is missing: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tool_calls`).Scan(&count); err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no tool_calls row without a resolvable parent, found %d", count)
	}

	var staged int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_blocked_on_user_spans WHERE parent_span_id = ?`, "span-tool-missing").Scan(&staged); err != nil {
		t.Fatalf("query pending_blocked_on_user_spans: %v", err)
	}
	if staged != 1 {
		t.Errorf("expected the child span to stage itself while its parent is missing, found %d staged rows", staged)
	}
}

// TestToolBlockedOnUserSpan_ChildArrivesBeforeParent covers the ordering
// live testing actually found: the blocked_on_user span landing (and being
// written) before its own parent claude_code.tool span in the same or an
// earlier flush. The child must stage itself and the parent's later arrival
// must claim the staged row rather than losing the enrichment.
func TestToolBlockedOnUserSpan_ChildArrivesBeforeParent(t *testing.T) {
	db := openTestDB(t)

	child := Span{
		TraceID:      "trace-1",
		SpanID:       "span-blocked-1",
		ParentSpanID: "span-tool-1",
		Name:         "claude_code.tool.blocked_on_user",
		StartNs:      1_200_000_000,
		EndNs:        1_800_000_000,
		Attrs:        map[string]any{"duration_ms": int64(600), "decision": "accept", "source": "user"},
	}
	if err := writeSpansBatch(db, []Span{child}); err != nil {
		t.Fatalf("write child span: %v", err)
	}

	parent := Span{
		TraceID: "trace-1",
		SpanID:  "span-tool-1",
		Name:    "claude_code.tool",
		StartNs: 1_000_000_000,
		EndNs:   2_000_000_000,
		Attrs:   map[string]any{"tool_use_id": "tu-1", "tool_name": "Bash"},
	}
	if err := writeSpansBatch(db, []Span{parent}); err != nil {
		t.Fatalf("write parent span: %v", err)
	}

	var blockedMs sql.NullInt64
	var decision, decisionSource sql.NullString
	if err := db.QueryRow(`SELECT blocked_on_user_ms, decision, decision_source FROM tool_calls WHERE tool_use_id = ?`, "tu-1").
		Scan(&blockedMs, &decision, &decisionSource); err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	if !blockedMs.Valid || blockedMs.Int64 != 600 {
		t.Errorf("blocked_on_user_ms = %v, want 600", blockedMs)
	}
	if !decision.Valid || decision.String != "accept" {
		t.Errorf("decision = %v, want accept", decision)
	}
	if !decisionSource.Valid || decisionSource.String != "user" {
		t.Errorf("decision_source = %v, want user", decisionSource)
	}

	var staged int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_blocked_on_user_spans`).Scan(&staged); err != nil {
		t.Fatalf("query pending_blocked_on_user_spans: %v", err)
	}
	if staged != 0 {
		t.Errorf("expected the staging row to be consumed once the parent claimed it, found %d remaining", staged)
	}
}

// --- metric points land alongside a session upsert ---

func TestMetricsWriter_UpsertsSession(t *testing.T) {
	db := openTestDB(t)

	p := MetricPoint{
		Ts:        1_700_000_000_000,
		Name:      "claude_code.token.usage",
		Value:     42,
		SessionID: "sess-1",
		Attrs:     map[string]any{"app.version": "2.1.212"},
	}
	if err := writeMetricsBatch(db, []MetricPoint{p}); err != nil {
		t.Fatalf("write metric: %v", err)
	}

	var appVersion sql.NullString
	if err := db.QueryRow(`SELECT app_version FROM sessions WHERE session_id = ?`, "sess-1").Scan(&appVersion); err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if !appVersion.Valid || appVersion.String != "2.1.212" {
		t.Errorf("app_version = %v, want 2.1.212", appVersion)
	}

	var value float64
	if err := db.QueryRow(`SELECT value FROM metric_points WHERE name = ?`, "claude_code.token.usage").Scan(&value); err != nil {
		t.Fatalf("query metric_points: %v", err)
	}
	if value != 42 {
		t.Errorf("value = %v, want 42", value)
	}
}
