package otlp

import (
	"reflect"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// Agents can send any OTLP value type, so each must become a plain Go value
// that ingest and adapters can read.
func TestAttrsToMap_EveryValueType(t *testing.T) {
	kv := func(k string, v *commonpb.AnyValue) *commonpb.KeyValue { return &commonpb.KeyValue{Key: k, Value: v} }
	got := attrsToMap([]*commonpb.KeyValue{
		kv("str", strVal("a")),
		kv("int", intVal(7)),
		kv("bool", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}),
		kv("double", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 1.5}}),
		kv("bytes", &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: []byte("hi")}}),
		kv("array", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{
			Values: []*commonpb.AnyValue{strVal("x"), intVal(2)},
		}}}),
		kv("map", &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
			Values: []*commonpb.KeyValue{kv("inner", strVal("y"))},
		}}}),
		kv("empty array", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{}}),
		kv("empty map", &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{}}),
		kv("no value", nil),
		kv("unset", &commonpb.AnyValue{}),
		nil,
	})
	want := map[string]any{
		"str":         "a",
		"int":         int64(7),
		"bool":        true,
		"double":      1.5,
		"bytes":       []byte("hi"),
		"array":       []any{"x", int64(2)},
		"map":         map[string]any{"inner": "y"},
		"empty array": nil,
		"empty map":   nil,
		"no value":    nil,
		"unset":       nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

func TestInt64Attr_AcceptsWholeDoubles(t *testing.T) {
	m := map[string]any{"i": int64(3), "f": float64(4), "s": "5"}
	for key, want := range map[string]int64{"i": 3, "f": 4} {
		if got, ok := int64Attr(m, key); !ok || got != want {
			t.Errorf("int64Attr(%q) = %d, %v", key, got, ok)
		}
	}
	if _, ok := int64Attr(m, "s"); ok {
		t.Error("a string isn't a number")
	}
}
