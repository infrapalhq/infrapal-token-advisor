package ingest

import (
	"database/sql"
	"encoding/json"
	"strconv"
)

// attrString looks up key in m and returns it as a string, or "" if absent
// or not a string.
func attrString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// attrInt64 looks up key in m and returns it as an int64. OTLP integer
// attributes decode as Go int64 directly (see otlp.anyValueToGo); the
// float64 case only matters if a value round-tripped through JSON first.
// The string case is a live-observed quirk: at least one numeric attribute
// (prompt_length, on a real 2.1.212 session) arrives OTLP-encoded as a
// StringValue rather than an IntValue, so a numeric-looking string is
// parsed rather than dropped.
func attrInt64(m map[string]any, key string) (int64, bool) {
	switch v := m[key].(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func attrFloat64(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func attrBool(m map[string]any, key string) (bool, bool) {
	b, ok := m[key].(bool)
	return b, ok
}

// nullableString wraps s as a NULL column when empty, so projection never
// stomps an already-known value with an empty string via COALESCE.
func nullableString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullStringAttr reads a string attribute directly into a NULL-safe SQL
// param.
func nullStringAttr(m map[string]any, key string) sql.NullString {
	return nullableString(attrString(m, key))
}

func nullInt64Attr(m map[string]any, key string) sql.NullInt64 {
	v, ok := attrInt64(m, key)
	return sql.NullInt64{Int64: v, Valid: ok}
}

func nullFloat64Attr(m map[string]any, key string) sql.NullFloat64 {
	v, ok := attrFloat64(m, key)
	return sql.NullFloat64{Float64: v, Valid: ok}
}

// nullBoolAttr reads a bool attribute into an INTEGER (0/1) column — SQLite
// has no native boolean type, matching how `success` etc. are declared in
// the schema.
func nullBoolAttr(m map[string]any, key string) sql.NullInt64 {
	v, ok := attrBool(m, key)
	if !ok {
		return sql.NullInt64{}
	}
	if v {
		return sql.NullInt64{Int64: 1, Valid: true}
	}
	return sql.NullInt64{Int64: 0, Valid: true}
}

// attrJSON renders an attribute as a JSON string for storage in a JSON TEXT
// column, whether the attribute arrived as an already-JSON-encoded string
// (Claude Code's usual gated-JSON convention, e.g. tool_input) or as a
// native nested value (kvlist/array attribute).
func attrJSON(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, s != ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func nullJSONAttr(m map[string]any, key string) sql.NullString {
	s, ok := attrJSON(m, key)
	return sql.NullString{String: s, Valid: ok}
}

// nullMapString reads a string field out of an already-decoded nested JSON
// object (see parseToolParameters), or an invalid NULL if m is nil or the
// field is absent.
func nullMapString(m map[string]any, key string) sql.NullString {
	if m == nil {
		return sql.NullString{}
	}
	v, ok := m[key].(string)
	return sql.NullString{String: v, Valid: ok && v != ""}
}
