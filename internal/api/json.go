package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/durfmt"
)

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a {"error": msg} JSON body with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSONBody decodes the request body's JSON into dst.
func decodeJSONBody(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// intParam reads an integer query parameter, falling back to def if absent
// or unparseable.
func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// sinceCutoffMs reads the ?since= query parameter (e.g. "7d", "30d") and
// returns the epoch-ms cutoff it implies. ok is false when the parameter is
// absent — the caller should apply no time filter in that case.
func sinceCutoffMs(r *http.Request) (cutoffMs int64, ok bool, err error) {
	since := r.URL.Query().Get("since")
	if since == "" {
		return 0, false, nil
	}
	d, err := durfmt.ParseSince(since)
	if err != nil {
		return 0, false, err
	}
	return time.Now().Add(-d).UnixMilli(), true, nil
}
