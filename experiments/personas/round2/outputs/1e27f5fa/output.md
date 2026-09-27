All tests pass, including the previously-failing `TestQuoteTierBoundary`.

**Summary**

- **Bug reproduced**: `TestQuoteTierBoundary` in rates_test.go failed — a 2000g parcel was quoted "up to 5000 g" (1250¢) instead of "up to 2000 g" (790¢); a 500g parcel had the same off-by-one issue.
- **Cause**: `bracketIndex` (brackets.go:10-17) used `v < limit` to pick a weight tier, but `Tier.UpToGrams` is defined as inclusive ("up to and including"), and `zones.go`/`letters.go` already use `<=` for their equivalent lookups. A parcel weighing exactly a tier's limit fell into the *next* (pricier) tier instead.
- **Fix**: changed the comparison to `v <= limit` in brackets.go, and removed the now-resolved TODO comment describing this exact off-by-one.
- **Verified**: full test suite passes (16/16), including the boundary test that now confirms both the 500g and 2000g cases price correctly.

Not touched (pre-existing, out of scope): the file `letters.go:42` has a comment noting its own tier loop duplicates `bracketIndex` and "could reuse it" — a legitimate dedup opportunity, but unrelated to this bug and left as a follow-up.
