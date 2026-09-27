All tests pass, including the previously-failing `TestQuoteTierBoundary`, and every other test (oversize, insurance, zones, letters) still passes untouched.

## Summary

**Bug reproduced**: `go test -run TestQuoteTierBoundary -v` failed — a 2000 g (2 kg) parcel was quoted the "up to 5000 g" tier (1250¢) instead of "up to 2000 g" (790¢).

**Cause**: `rates.go:93` (previously) looked up the weight tier with `bracketIndex`, whose limits are exclusive (`v < limit`, `brackets.go:12`). But `Tier.UpToGrams` is documented as inclusive: "up to and including" (`rates.go:27-28`). At exactly 2000 g, `2000 < 2000` is false, so it spilled into the next tier.

**Fix** (`rates.go`, in `Quote`): replaced the `bracketIndex` call with an inline inclusive-limit search over `tiers`. This only changes weight-tier lookup.

**Why `bracketIndex` itself was left alone**: it's shared by `OversizeFee` (`surcharges.go:37-39`) and `InsuranceFee` (`insurance.go:23-25`), both of which explicitly document and rely on its exclusive-limit behavior ("$100.00 is not under $100.00"). Changing the shared helper would have silently broken those two correct callers.

**Not changed** (follow-ups, not in scope):
- `brackets.go:7-9` has a stale TODO claiming limits "should be inclusive" — that's now contradicted by the surcharge/insurance callers' documented intent, and worth a follow-up cleanup so nobody "fixes" `bracketIndex` and breaks those two.
- `letters.go:42` notes it duplicates this same inclusive-lookup pattern inline; could be shared with the new `rates.go` loop, but that's a refactor, not this fix.
