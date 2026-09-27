All tests pass, including insurance and oversize tests, confirming the exclusive-limit behavior they depend on is untouched.

**Root cause:** `bracketIndex` in `brackets.go` used an exclusive comparison (`v < limit`), but `Tier.UpToGrams` in `rates.go` is documented and used as *inclusive* ("up to and including"). So a 2000g parcel failed `2000 < 2000` and fell through to the next tier (5000g/5kg pricing).

I couldn't just flip `bracketIndex`'s comparison globally — `insurance.go` and `surcharges.go` explicitly rely on its exclusive semantics (e.g. exactly $100.00 is not "under $100", exactly 300cm is already oversize). So I:
- Added a new `bracketIndexInclusive` helper in `brackets.go` alongside the existing `bracketIndex`.
- Switched the weight-tier lookup in `rates.go` to use it.
- Added a test for the new helper.

The existing `TestQuoteTierBoundary` test (which was already failing at 500g and 2000g boundaries) now passes, and the full suite is green.
