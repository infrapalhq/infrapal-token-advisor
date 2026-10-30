package otlp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// resourceLogsFieldNumber is the field number of `repeated ResourceLogs
// resource_logs = 1` on collector.logs.v1.ExportLogsServiceRequest.
//
// We deliberately don't import that generated package: its .pb.gw.go file
// pulls in grpc-gateway and grpc-go, which the project explicitly avoids
// for a loopback-only http/protobuf receiver. The collector request
// message is just a thin envelope around ResourceLogs (from logs/v1,
// which has no gRPC dependency), so we unwrap it by hand with protowire.
const resourceLogsFieldNumber = protowire.Number(1)

// decodeLogsRequest unwraps an ExportLogsServiceRequest's top-level
// `resource_logs` field without depending on the generated collector
// package, then flattens every log record into a RawEvent.
//
// Nothing here interprets the event beyond the handful of correlation
// fields (event_name, session_id, prompt_id, seq, ts) that events_raw
// indexes on — the full decoded record goes into Body verbatim so
// projection can read anything else it needs, and so
// nothing is lost if Claude Code adds an attribute we don't know about yet.
func decodeLogsRequest(data []byte) ([]ingest.RawEvent, error) {
	var events []ingest.RawEvent
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("consume tag: %w", protowire.ParseError(n))
		}
		data = data[n:]

		if typ != protowire.BytesType {
			n := protowire.ConsumeFieldValue(num, typ, data)
			if n < 0 {
				return nil, fmt.Errorf("skip field %d: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			continue
		}

		v, n := protowire.ConsumeBytes(data)
		if n < 0 {
			return nil, fmt.Errorf("consume bytes for field %d: %w", num, protowire.ParseError(n))
		}
		data = data[n:]

		if num != resourceLogsFieldNumber {
			continue
		}

		var rl logspb.ResourceLogs
		if err := proto.Unmarshal(v, &rl); err != nil {
			return nil, fmt.Errorf("unmarshal ResourceLogs: %w", err)
		}
		events = append(events, decodeResourceLogs(&rl)...)
	}
	return events, nil
}

func decodeResourceLogs(rl *logspb.ResourceLogs) []ingest.RawEvent {
	var events []ingest.RawEvent
	resourceAttrs := attrsToMap(rl.GetResource().GetAttributes())
	for _, sl := range rl.GetScopeLogs() {
		for _, lr := range sl.GetLogRecords() {
			events = append(events, decodeLogRecord(lr, resourceAttrs))
		}
	}
	return events
}

func decodeLogRecord(lr *logspb.LogRecord, resourceAttrs map[string]any) ingest.RawEvent {
	recordAttrs := attrsToMap(lr.GetAttributes())

	merged := make(map[string]any, len(resourceAttrs)+len(recordAttrs))
	for k, v := range resourceAttrs {
		merged[k] = v
	}
	for k, v := range recordAttrs {
		merged[k] = v
	}

	seq, _ := int64Attr(merged, "event.sequence")

	ts := lr.GetTimeUnixNano()
	if ts == 0 {
		ts = lr.GetObservedTimeUnixNano()
	}

	var traceID, spanID string
	if len(lr.GetTraceId()) > 0 {
		traceID = hex.EncodeToString(lr.GetTraceId())
	}
	if len(lr.GetSpanId()) > 0 {
		spanID = hex.EncodeToString(lr.GetSpanId())
	}

	body := map[string]any{
		"resource":                resourceAttrs,
		"attributes":              recordAttrs,
		"severity_text":           lr.GetSeverityText(),
		"time_unix_nano":          lr.GetTimeUnixNano(),
		"observed_time_unix_nano": lr.GetObservedTimeUnixNano(),
	}
	if traceID != "" {
		body["trace_id"] = traceID
	}
	if spanID != "" {
		body["span_id"] = spanID
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		// json.Marshal only fails here on unsupported types, which
		// attrsToMap never produces — but degrade rather than drop.
		bodyJSON = []byte(`{"error":"failed to encode log record"}`)
	}

	return ingest.RawEvent{
		Ts:            int64(ts / 1e6), // ns -> ms
		SessionID:     stringAttr(merged, "session.id"),
		PromptID:      stringAttr(merged, "prompt.id"),
		EventName:     stringAttr(merged, "event.name"),
		Seq:           seq,
		TraceID:       traceID,
		SpanID:        spanID,
		Attrs:         merged,
		ResourceAttrs: resourceAttrs,
		Body:          string(bodyJSON),
	}
}
