// Package durfmt parses the shorthand duration strings ("7d", "24h", "2w")
// used by both the CLI's --since/--before flags and the HTTP API's
// ?since= query parameter.
package durfmt

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince parses durations like "7d", "24h", "30m", "2w".
// time.ParseDuration doesn't support "d" or "w", so days and weeks are
// handled here and everything else is delegated.
func ParseSince(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	unit := s[len(s)-1]
	switch unit {
	case 'd', 'w':
		n, err := strconv.ParseFloat(strings.TrimSuffix(s, string(unit)), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		days := n
		if unit == 'w' {
			days *= 7
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	default:
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (expected forms like 7d, 24h, 30m)", s)
		}
		return d, nil
	}
}
