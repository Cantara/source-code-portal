package badges

import (
	"strconv"
	"strings"
	"time"
)

// formatCount abbreviates large counts: 999, 1k, 1.2k, 12k, 999k, 1M, 1.2M.
func formatCount(n int) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return abbreviate(n, 1000, "k")
	}
	return abbreviate(n, 1_000_000, "M")
}

func abbreviate(n, unit int, suffix string) string {
	tenths := n * 10 / unit // truncate, never round up across a unit boundary
	if tenths >= 1000 {     // 999.9k stays 999k
		tenths = 999 * 10
	}
	if tenths < 100 {
		s := strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10)
		return strings.TrimSuffix(s, ".0") + suffix
	}
	return strconv.Itoa(tenths/10) + suffix
}

// relativeDay renders the age of t at now in UTC calendar days: today, yesterday, N days ago (2..13), N weeks ago
// (2..8 weeks), N months ago (2..23), N years ago. A future t (clock skew) is "today".
func relativeDay(now, t time.Time) (string, int) {
	days := civilDay(now) - civilDay(t)
	switch {
	case days <= 0:
		return "today", 0
	case days == 1:
		return "yesterday", 1
	case days < 14:
		return strconv.Itoa(days) + " days ago", days
	case days < 60:
		return strconv.Itoa(days/7) + " weeks ago", days
	case days < 730:
		return strconv.Itoa(days/30) + " months ago", days
	}
	return strconv.Itoa(days/365) + " years ago", days
}

// civilDay is the number of days since 1970-01-01 of the UTC calendar date of t.
func civilDay(t time.Time) int {
	y, m, d := t.UTC().Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}
