Fixed.

**Bug:** `FormatCents` in money.go:11-15 didn't handle negative amounts. For `-1250`, integer division/modulo truncate toward zero (`-1250/100 = -12`, `-1250%100 = -50`), and the `-` sign ended up embedded in both `dollars` (via `groupThousands`) and `rem` independently, producing `"$-12.-50"` instead of a single leading sign.

**Fix (money.go:11-18):** extract the sign once up front, work with the absolute value for the digit formatting, and prepend the sign before `$`:

```go
func FormatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	dollars := cents / 100
	rem := cents % 100
	return sign + "$" + groupThousands(dollars) + fmt.Sprintf(".%02d", rem)
}
```

**Verification:** money_test.go already contained the regression cases (`-1250 → "-$12.50"`, `-50 → "-$0.50"`, `-123456 → "-$1,234.56"`); they failed before the fix (`go test -run TestFormatCents -v` showed `"$-12.-50"` etc.) and the full suite passes now.

Not touched: `round_cents_legacy` and the `TODO(2019)` currency comment — unrelated to this bug, left as-is.
