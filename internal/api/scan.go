package api

import "database/sql"

// rowsToMaps scans every row into a map[string]any keyed by column name.
// Every handler in this package returns query results shaped directly by
// their SQL (mostly straight off the derived views), so a generic scan
// avoids hand-writing a struct + Scan call per endpoint.
func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			m[c] = normalizeValue(vals[i])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// normalizeValue converts driver values ([]byte for TEXT columns under
// modernc.org/sqlite) into JSON-friendly types.
func normalizeValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
