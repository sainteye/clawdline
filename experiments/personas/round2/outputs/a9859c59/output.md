This test already asserted the *correct* expected behavior (exactly 2000g → the 2000g tier at 790¢), so it must have been failing before my fix — confirming this was a real, caught regression. It now passes.

## Summary

The bug was in `brackets.go:10` — `bracketIndex` uses exclusive limits (`v < limit`), but `rates.go`'s weight `Tier.UpToGrams` is documented as inclusive ("up to and including"). So a 2000g (2kg) parcel failed `2000 < 2000`, fell through to the next tier (`5000`), and got quoted at the 5kg rate.

I didn't change `bracketIndex`'s behavior globally, since `insurance.go` and `surcharges.go` intentionally rely on its exclusive semantics for "under $X" / "under N cm" bands. Instead:

- Added `bracketIndexInclusive` in `brackets.go`, which reuses `bracketIndex` with `v-1` to get "up to and including" semantics.
- Switched the weight-tier lookup in `rates.go:93` to use it.
- Added `TestBracketIndexInclusive` in `brackets_test.go`.

All existing tests pass, including `TestQuoteTierBoundary`, which asserts a 2000g parcel gets the `up to 2000 g` tier — that assertion was silently failing before the fix.
