package ledger

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatCents renders an amount in cents as a US dollar string, e.g. 123456 -> "$1,234.56".
func FormatCents(cents int64) string {
	dollars := cents / 100
	rem := cents % 100
	return "$" + groupThousands(dollars) + fmt.Sprintf(".%02d", rem)
}

// TODO(2019): support currencies other than USD.

// groupThousands inserts a comma every three digits.
func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// RoundCents converts a dollar amount to whole cents, rounding half away from zero.
func RoundCents(dollars float64) int64 {
	return int64(math.Round(dollars * 100))
}

// Deprecated: use RoundCents. Kept for the old CSV importer.
func round_cents_legacy(d float64) int64 {
	return int64(d*100 + 0.5)
}

// fmt_percent renders basis points as a percentage, e.g. 1250 -> "12.50%".
func fmt_percent(bp int) string {
	return fmt.Sprintf("%d.%02d%%", bp/100, bp%100)
}
