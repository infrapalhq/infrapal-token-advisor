// Package otlp decodes OTLP http/protobuf payloads emitted by Claude Code
// and turns them into the raw event records the ingest pipeline persists.
package otlp

import (
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// attrsToMap flattens an OTLP KeyValue slice into a plain Go map, suitable
// for JSON encoding into events_raw.body. Duplicate keys keep the last
// value, matching how most OTel SDKs treat duplicate attributes.
func attrsToMap(kvs []*commonpb.KeyValue) map[string]any {
	if len(kvs) == 0 {
		return nil
	}
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		if kv == nil {
			continue
		}
		m[kv.Key] = anyValueToGo(kv.Value)
	}
	return m
}

// anyValueToGo converts an OTLP AnyValue oneof into a plain Go value.
func anyValueToGo(v *commonpb.AnyValue) any {
	if v == nil {
		return nil
	}
	switch val := v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_BoolValue:
		return val.BoolValue
	case *commonpb.AnyValue_IntValue:
		return val.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return val.DoubleValue
	case *commonpb.AnyValue_ArrayValue:
		if val.ArrayValue == nil {
			return nil
		}
		arr := make([]any, len(val.ArrayValue.Values))
		for i, e := range val.ArrayValue.Values {
			arr[i] = anyValueToGo(e)
		}
		return arr
	case *commonpb.AnyValue_KvlistValue:
		if val.KvlistValue == nil {
			return nil
		}
		return attrsToMap(val.KvlistValue.Values)
	case *commonpb.AnyValue_BytesValue:
		return val.BytesValue
	default:
		return nil
	}
}

// stringAttr looks up key in m and returns it as a string, or "" if absent
// or not a string.
func stringAttr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// int64Attr looks up key in m and returns it as an int64. Claude Code's
// SDK may emit small integer attributes as either IntValue (int64) or,
// after a JSON round-trip in tests, float64 — handle both.
func int64Attr(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
