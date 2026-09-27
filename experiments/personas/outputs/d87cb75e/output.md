All tests pass. `int64` has no `-math.MinInt64` overflow concern here since cents amounts never approach that range.

**Fix**: `money.go:11-18` (`FormatCents`) — extract the sign before dividing, negate `cents` to a positive value, then compute `dollars`/`rem` from that and prefix the sign onto the whole string instead of letting it leak into both halves independently.

**Not changed** (noticed but out of scope): `round_cents_legacy` and `fmt_percent` are unrelated dead/misnamed code — left alone since the ask was specifically the invoice sign bug.
