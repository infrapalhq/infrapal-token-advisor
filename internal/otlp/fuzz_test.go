package otlp

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// A local process can send the collector anything, so a bad payload may be
// rejected but must never panic: that would take telemetry down for every agent.
func seedPayloads(f *testing.F) {
	f.Helper()
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	record, err := proto.Marshal(&logspb.ResourceLogs{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{{Key: "service.name", Value: strVal("claude-code")}}},
		ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: 1,
			Attributes:   []*commonpb.KeyValue{{Key: "session.id", Value: strVal("s")}},
		}}}},
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(protowire.AppendBytes(protowire.AppendTag(nil, resourceLogsFieldNumber, protowire.BytesType), record))
}

func FuzzDecodeLogsRequest(f *testing.F) {
	seedPayloads(f)
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = decodeLogsRequest(data)
	})
}

func FuzzDecodeMetricsRequest(f *testing.F) {
	seedPayloads(f)
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = decodeMetricsRequest(data)
	})
}

func FuzzDecodeTracesRequest(f *testing.F) {
	seedPayloads(f)
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = decodeTracesRequest(data)
	})
}
