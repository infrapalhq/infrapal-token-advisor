package otlp

import (
	"fmt"

	"github.com/ShamikMukhopadhyay/ccotel/internal/ingest"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// resourceMetricsFieldNumber is the field number of `repeated
// ResourceMetrics resource_metrics = 1` on
// collector.metrics.v1.ExportMetricsServiceRequest. We unwrap it by hand
// with protowire rather than importing the generated collector package,
// same grpc-avoidance rationale as decodeLogsRequest in logs.go.
const resourceMetricsFieldNumber = protowire.Number(1)

// decodeMetricsRequest unwraps an ExportMetricsServiceRequest's top-level
// `resource_metrics` field and flattens every data point into a
// MetricPoint. Claude Code's metrics are always delta-temporality Sum or
// Gauge counters (see the schema's gotcha #1: sum them, don't diff them) —
// Histogram/ExponentialHistogram/Summary metrics aren't part of its
// documented schema and are skipped rather than guessed at.
func decodeMetricsRequest(data []byte) ([]ingest.MetricPoint, error) {
	var points []ingest.MetricPoint
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

		if num != resourceMetricsFieldNumber {
			continue
		}

		var rm metricspb.ResourceMetrics
		if err := proto.Unmarshal(v, &rm); err != nil {
			return nil, fmt.Errorf("unmarshal ResourceMetrics: %w", err)
		}
		points = append(points, decodeResourceMetrics(&rm)...)
	}
	return points, nil
}

func decodeResourceMetrics(rm *metricspb.ResourceMetrics) []ingest.MetricPoint {
	var points []ingest.MetricPoint
	resourceAttrs := attrsToMap(rm.GetResource().GetAttributes())
	for _, sm := range rm.GetScopeMetrics() {
		for _, m := range sm.GetMetrics() {
			points = append(points, decodeMetric(m, resourceAttrs)...)
		}
	}
	return points
}

func decodeMetric(m *metricspb.Metric, resourceAttrs map[string]any) []ingest.MetricPoint {
	metadataAttrs := attrsToMap(m.GetMetadata())

	var dps []*metricspb.NumberDataPoint
	switch {
	case m.GetSum() != nil:
		dps = m.GetSum().GetDataPoints()
	case m.GetGauge() != nil:
		dps = m.GetGauge().GetDataPoints()
	default:
		return nil
	}

	points := make([]ingest.MetricPoint, 0, len(dps))
	for _, dp := range dps {
		dpAttrs := attrsToMap(dp.GetAttributes())

		merged := make(map[string]any, len(resourceAttrs)+len(metadataAttrs)+len(dpAttrs))
		for k, v := range resourceAttrs {
			merged[k] = v
		}
		for k, v := range metadataAttrs {
			merged[k] = v
		}
		for k, v := range dpAttrs {
			merged[k] = v
		}

		var value float64
		switch dp.GetValue().(type) {
		case *metricspb.NumberDataPoint_AsDouble:
			value = dp.GetAsDouble()
		case *metricspb.NumberDataPoint_AsInt:
			value = float64(dp.GetAsInt())
		}

		points = append(points, ingest.MetricPoint{
			Ts:            int64(dp.GetTimeUnixNano() / 1e6), // ns -> ms
			Name:          m.GetName(),
			Value:         value,
			SessionID:     stringAttr(merged, "session.id"),
			Model:         stringAttr(merged, "model"),
			Attrs:         merged,
			ResourceAttrs: resourceAttrs,
		})
	}
	return points
}
