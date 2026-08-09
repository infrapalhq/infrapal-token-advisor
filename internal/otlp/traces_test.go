package otlp

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func buildTracesEnvelope(t *testing.T, rss ...*tracepb.ResourceSpans) []byte {
	t.Helper()
	var out []byte
	for _, rs := range rss {
		b, err := proto.Marshal(rs)
		if err != nil {
			t.Fatalf("marshal ResourceSpans: %v", err)
		}
		out = protowire.AppendTag(out, resourceSpansFieldNumber, protowire.BytesType)
		out = protowire.AppendBytes(out, b)
	}
	return out
}

func TestDecodeTracesRequest_Span(t *testing.T) {
	rs := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			{Key: "session.id", Value: strVal("sess-1")},
		}},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				TraceId:           []byte{0x01, 0x02},
				SpanId:            []byte{0x03, 0x04},
				ParentSpanId:      []byte{0x05, 0x06},
				Name:              "claude_code.tool",
				StartTimeUnixNano: 1_000,
				EndTimeUnixNano:   2_000,
				Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
				Attributes: []*commonpb.KeyValue{
					{Key: "tool_use_id", Value: strVal("tu-1")},
				},
			}},
		}},
	}

	spans, err := decodeTracesRequest(buildTracesEnvelope(t, rs))
	if err != nil {
		t.Fatalf("decodeTracesRequest: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]
	if s.TraceID != "0102" {
		t.Errorf("TraceID = %q, want 0102", s.TraceID)
	}
	if s.SpanID != "0304" {
		t.Errorf("SpanID = %q, want 0304", s.SpanID)
	}
	if s.ParentSpanID != "0506" {
		t.Errorf("ParentSpanID = %q, want 0506", s.ParentSpanID)
	}
	if s.Name != "claude_code.tool" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Status != "OK" {
		t.Errorf("Status = %q, want OK", s.Status)
	}
	if s.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", s.SessionID)
	}
	if s.Attrs["tool_use_id"] != "tu-1" {
		t.Errorf("Attrs[tool_use_id] = %v, want tu-1", s.Attrs["tool_use_id"])
	}
}

func TestDecodeTracesRequest_StatusCodes(t *testing.T) {
	mk := func(code tracepb.Status_StatusCode) *tracepb.ResourceSpans {
		return &tracepb.ResourceSpans{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: []byte{0x01},
					SpanId:  []byte{0x02},
					Status:  &tracepb.Status{Code: code},
				}},
			}},
		}
	}

	for _, tc := range []struct {
		code tracepb.Status_StatusCode
		want string
	}{
		{tracepb.Status_STATUS_CODE_UNSET, "UNSET"},
		{tracepb.Status_STATUS_CODE_OK, "OK"},
		{tracepb.Status_STATUS_CODE_ERROR, "ERROR"},
	} {
		spans, err := decodeTracesRequest(buildTracesEnvelope(t, mk(tc.code)))
		if err != nil {
			t.Fatalf("decodeTracesRequest: %v", err)
		}
		if len(spans) != 1 {
			t.Fatalf("expected 1 span, got %d", len(spans))
		}
		if spans[0].Status != tc.want {
			t.Errorf("code %v: Status = %q, want %q", tc.code, spans[0].Status, tc.want)
		}
	}
}

func TestDecodeTracesRequest_NoParentSpan(t *testing.T) {
	rs := &tracepb.ResourceSpans{
		ScopeSpans: []*tracepb.ScopeSpans{{
			Spans: []*tracepb.Span{{
				TraceId: []byte{0x01},
				SpanId:  []byte{0x02},
				Name:    "claude_code.interaction",
			}},
		}},
	}

	spans, err := decodeTracesRequest(buildTracesEnvelope(t, rs))
	if err != nil {
		t.Fatalf("decodeTracesRequest: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].ParentSpanID != "" {
		t.Errorf("ParentSpanID = %q, want empty for a root span", spans[0].ParentSpanID)
	}
}
