All tests pass, including the oversize/insurance tests, confirming those callers' exclusive-limit behavior is untouched.

## Summary

**Bug**: `Quote()` in `rates.go:93` looked up a parcel's weight tier with `bracketIndex`, which uses strict `<` (exclusive limits). A 2000g parcel isn't `< 2000`, so it fell into the next bracket up (`UpToGrams: 5000`), billing it at the 5kg price (1250¢) instead of the 2kg price (790¢).

**Cause**: `Tier.UpToGrams` is documented as inclusive ("up to and including", `rates.go:27-28`), but `bracketIndex` (`brackets.go:10-17`) is exclusive. `bracketIndex` is also used by `OversizeFee` (`surcharges.go:39`) and `InsuranceFee` (`insurance.go:25`), both of which explicitly rely on the exclusive "under $X" semantics — so I fixed the caller that used it wrongly rather than changing the shared function.

**Fix**: `rates.go:87-96` now does its own inclusive-limit tier lookup (`grams <= t.UpToGrams`) instead of reusing `bracketIndex`.

**Verification**: `TestQuoteTierBoundary` (already in `rates_test.go`, pre-existing) failed before the fix — a 2000g parcel got "up to 5000 g" / 1250¢ instead of "up to 2000 g" / 790¢ — and passes after. Full suite (`go test ./...`, 16 tests) is green, including `TestOversizeFee` and `TestInsuranceFee`, confirming the other two `bracketIndex` callers are unaffected.

**Left alone**: `brackets.go:7-9`'s TODO still says "weight tiers are off by one at the edges" — that's now stale for `rates.go` specifically (since it no longer uses `bracketIndex`), though the general observation about `bracketIndex` being exclusive-only still applies to its remaining callers. Worth a follow-up comment cleanup, but out of scope for this fix.
