package cli

import (
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/durfmt"
)

// parseSince parses durations like "7d", "24h", "30m", "2w" — the shorthand
// used by --since/--before flags. See internal/durfmt, shared with the HTTP
// API's ?since= query parameter.
func parseSince(s string) (time.Duration, error) {
	return durfmt.ParseSince(s)
}
