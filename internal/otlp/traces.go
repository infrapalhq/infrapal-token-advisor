package otlp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// resourceSpansFieldNumber is the field number of `repeated ResourceSpans
// resource_spans = 1` on collector.trace.v1.ExportTraceServiceRequest. We
// unwrap it by hand with protowire rather than importing the generated
// collector package, same grpc-avoidance rationale as decodeLogsRequest.
const resourceSpansFieldNumber = protowire.Number(1)

// decodeTracesRequest unwraps an ExportTraceServiceRequest's top-level
// `resource_spans` field and flattens every span into an ingest.Span.
func decodeTracesRequest(data []byte) ([]ingest.Span, error) {
	var spans []ingest.Span
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

		if num != resourceSpansFieldNumber {
			continue
		}

		var rs tracepb.ResourceSpans
		if err := proto.Unmarshal(v, &rs); err != nil {
			return nil, fmt.Errorf("unmarshal ResourceSpans: %w", err)
		}
		spans = append(spans, decodeResourceSpans(&rs)...)
	}
	return spans, nil
}

func decodeResourceSpans(rs *tracepb.ResourceSpans) []ingest.Span {
	var spans []ingest.Span
	resourceAttrs := attrsToMap(rs.GetResource().GetAttributes())
	for _, ss := range rs.GetScopeSpans() {
		for _, sp := range ss.GetSpans() {
			spans = append(spans, decodeSpan(sp, resourceAttrs))
		}
	}
	return spans
}

func decodeSpan(sp *tracepb.Span, resourceAttrs map[string]any) ingest.Span {
	spanAttrs := attrsToMap(sp.GetAttributes())

	merged := make(map[string]any, len(resourceAttrs)+len(spanAttrs))
	for k, v := range resourceAttrs {
		merged[k] = v
	}
	for k, v := range spanAttrs {
		merged[k] = v
	}

	status := "UNSET"
	switch sp.GetStatus().GetCode() {
	case tracepb.Status_STATUS_CODE_OK:
		status = "OK"
	case tracepb.Status_STATUS_CODE_ERROR:
		status = "ERROR"
	}

	var parentSpanID string
	if len(sp.GetParentSpanId()) > 0 {
		parentSpanID = hex.EncodeToString(sp.GetParentSpanId())
	}

	body := map[string]any{
		"resource":   resourceAttrs,
		"attributes": spanAttrs,
		"kind":       sp.GetKind().String(),
		"status":     status,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		// json.Marshal only fails here on unsupported types, which
		// attrsToMap never produces — but degrade rather than drop.
		bodyJSON = []byte(`{"error":"failed to encode span"}`)
	}

	return ingest.Span{
		TraceID:       hex.EncodeToString(sp.GetTraceId()),
		SpanID:        hex.EncodeToString(sp.GetSpanId()),
		ParentSpanID:  parentSpanID,
		Name:          sp.GetName(), // Claude Code spans are named after their span.type
		StartNs:       int64(sp.GetStartTimeUnixNano()),
		EndNs:         int64(sp.GetEndTimeUnixNano()),
		Status:        status,
		SessionID:     stringAttr(merged, "session.id"),
		Attrs:         merged,
		ResourceAttrs: resourceAttrs,
		Body:          string(bodyJSON),
	}
}
