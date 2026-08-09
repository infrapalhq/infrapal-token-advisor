package otlp

import (
	"encoding/json"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func strVal(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func intVal(i int64) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: i}}
}

// buildEnvelope hand-encodes the collector-level ExportLogsServiceRequest
// envelope (repeated ResourceLogs resource_logs = 1) around rls, mirroring
// what a real OTel SDK exporter would send over the wire — without pulling
// in the generated collector package (see the comment on
// resourceLogsFieldNumber in logs.go for why).
func buildEnvelope(t *testing.T, rls ...*logspb.ResourceLogs) []byte {
	t.Helper()
	var out []byte
	for _, rl := range rls {
		b, err := proto.Marshal(rl)
		if err != nil {
			t.Fatalf("marshal ResourceLogs: %v", err)
		}
		out = protowire.AppendTag(out, resourceLogsFieldNumber, protowire.BytesType)
		out = protowire.AppendBytes(out, b)
	}
	return out
}

func TestDecodeLogsRequest_UserPrompt(t *testing.T) {
	resource := &resourcepb.Resource{
		Attributes: []*commonpb.KeyValue{
			{Key: "session.id", Value: strVal("sess-1")},
			{Key: "app.version", Value: strVal("2.1.212")},
		},
	}
	record := &logspb.LogRecord{
		TimeUnixNano: 1_700_000_000_000_000_000,
		Attributes: []*commonpb.KeyValue{
			{Key: "event.name", Value: strVal("claude_code.user_prompt")},
			{Key: "event.sequence", Value: intVal(1)},
			{Key: "prompt.id", Value: strVal("prompt-1")},
			{Key: "prompt_length", Value: intVal(42)},
		},
	}
	rl := &logspb.ResourceLogs{
		Resource:  resource,
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{record}}},
	}

	events, err := decodeLogsRequest(buildEnvelope(t, rl))
	if err != nil {
		t.Fatalf("decodeLogsRequest: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventName != "claude_code.user_prompt" {
		t.Errorf("EventName = %q, want claude_code.user_prompt", e.EventName)
	}
	if e.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", e.SessionID)
	}
	if e.PromptID != "prompt-1" {
		t.Errorf("PromptID = %q, want prompt-1", e.PromptID)
	}
	if e.Seq != 1 {
		t.Errorf("Seq = %d, want 1", e.Seq)
	}
	if want := int64(1_700_000_000_000); e.Ts != want {
		t.Errorf("Ts = %d, want %d", e.Ts, want)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
		t.Fatalf("Body is not valid JSON: %v", err)
	}
	attrs, ok := body["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("Body missing attributes map: %v", body)
	}
	if attrs["prompt_length"].(float64) != 42 {
		t.Errorf("Body.attributes.prompt_length = %v, want 42", attrs["prompt_length"])
	}
	resourceAttrs, ok := body["resource"].(map[string]any)
	if !ok || resourceAttrs["app.version"] != "2.1.212" {
		t.Errorf("Body.resource.app.version missing or wrong: %v", body["resource"])
	}
}

func TestDecodeLogsRequest_UnknownAttributesDontBreakDecode(t *testing.T) {
	record := &logspb.LogRecord{
		TimeUnixNano: 1_700_000_000_000_000_000,
		Attributes: []*commonpb.KeyValue{
			{Key: "event.name", Value: strVal("claude_code.some_future_event")},
			{Key: "totally.unknown.attribute", Value: strVal("surprise")},
		},
	}
	rl := &logspb.ResourceLogs{
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{record}}},
	}

	events, err := decodeLogsRequest(buildEnvelope(t, rl))
	if err != nil {
		t.Fatalf("decodeLogsRequest: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventName != "claude_code.some_future_event" {
		t.Errorf("EventName = %q", events[0].EventName)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(events[0].Body), &body); err != nil {
		t.Fatalf("Body is not valid JSON: %v", err)
	}
	attrs := body["attributes"].(map[string]any)
	if attrs["totally.unknown.attribute"] != "surprise" {
		t.Errorf("unknown attribute lost, got body: %v", body)
	}
}

func TestDecodeLogsRequest_MultipleResourceLogs(t *testing.T) {
	mk := func(session string) *logspb.ResourceLogs {
		return &logspb.ResourceLogs{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "session.id", Value: strVal(session)}}},
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{
				{Attributes: []*commonpb.KeyValue{{Key: "event.name", Value: strVal("claude_code.tool_result")}}},
			}}},
		}
	}

	events, err := decodeLogsRequest(buildEnvelope(t, mk("a"), mk("b")))
	if err != nil {
		t.Fatalf("decodeLogsRequest: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].SessionID != "a" || events[1].SessionID != "b" {
		t.Errorf("got sessions %q, %q", events[0].SessionID, events[1].SessionID)
	}
}
