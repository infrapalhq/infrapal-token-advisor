package otlp

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func buildMetricsEnvelope(t *testing.T, rms ...*metricspb.ResourceMetrics) []byte {
	t.Helper()
	var out []byte
	for _, rm := range rms {
		b, err := proto.Marshal(rm)
		if err != nil {
			t.Fatalf("marshal ResourceMetrics: %v", err)
		}
		out = protowire.AppendTag(out, resourceMetricsFieldNumber, protowire.BytesType)
		out = protowire.AppendBytes(out, b)
	}
	return out
}

func TestDecodeMetricsRequest_Sum(t *testing.T) {
	rm := &metricspb.ResourceMetrics{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
			{Key: "session.id", Value: strVal("sess-1")},
		}},
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "claude_code.token.usage",
				Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{
					DataPoints: []*metricspb.NumberDataPoint{{
						TimeUnixNano: 1_700_000_000_000_000_000,
						Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 123},
						Attributes: []*commonpb.KeyValue{
							{Key: "model", Value: strVal("claude-sonnet-5")},
						},
					}},
				}},
			}},
		}},
	}

	points, err := decodeMetricsRequest(buildMetricsEnvelope(t, rm))
	if err != nil {
		t.Fatalf("decodeMetricsRequest: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}

	p := points[0]
	if p.Name != "claude_code.token.usage" {
		t.Errorf("Name = %q", p.Name)
	}
	if p.Value != 123 {
		t.Errorf("Value = %v, want 123", p.Value)
	}
	if p.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", p.SessionID)
	}
	if p.Model != "claude-sonnet-5" {
		t.Errorf("Model = %q, want claude-sonnet-5", p.Model)
	}
	if want := int64(1_700_000_000_000); p.Ts != want {
		t.Errorf("Ts = %d, want %d", p.Ts, want)
	}
}

func TestDecodeMetricsRequest_Gauge(t *testing.T) {
	rm := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "claude_code.active_time.total",
				Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
					DataPoints: []*metricspb.NumberDataPoint{{
						Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 1.5},
					}},
				}},
			}},
		}},
	}

	points, err := decodeMetricsRequest(buildMetricsEnvelope(t, rm))
	if err != nil {
		t.Fatalf("decodeMetricsRequest: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	if points[0].Value != 1.5 {
		t.Errorf("Value = %v, want 1.5", points[0].Value)
	}
}

func TestDecodeMetricsRequest_HistogramSkipped(t *testing.T) {
	rm := &metricspb.ResourceMetrics{
		ScopeMetrics: []*metricspb.ScopeMetrics{{
			Metrics: []*metricspb.Metric{{
				Name: "some.histogram",
				Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
					DataPoints: []*metricspb.HistogramDataPoint{{}},
				}},
			}},
		}},
	}

	points, err := decodeMetricsRequest(buildMetricsEnvelope(t, rm))
	if err != nil {
		t.Fatalf("decodeMetricsRequest: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("expected histogram metrics to be skipped, got %d points", len(points))
	}
}
