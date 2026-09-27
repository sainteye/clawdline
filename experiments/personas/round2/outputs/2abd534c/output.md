All tests pass, including `TestQuoteTierBoundary` which now correctly asserts a 2000 g parcel gets the 2000 g tier price (790¢), not the 5000 g tier (1250¢).

**Root cause:** `Tier.UpToGrams` in `rates.go` is documented as inclusive ("up to and including"), but its value was passed directly into `bracketIndex`, which treats limits as *exclusive* (that's deliberately correct for `OversizeFee` and `InsuranceFee` elsewhere — see their comments). This meant every weight exactly on a tier boundary (500 g, 1000 g, 2000 g, 5000 g, 10000 g, 20000 g) fell into the *next* tier up.

**Fix:** in `Quote` (`rates.go`), shift each tier's limit by one (`tiers[i].UpToGrams + 1`) before passing it to `bracketIndex`, converting the inclusive bound into the exclusive form the helper expects. Also updated the stale TODO comment on `bracketIndex` to describe its actual (correct, exclusive) contract instead of proposing to change it — changing `bracketIndex` itself would have broken the insurance and oversize surcharge lookups, which correctly depend on exclusive semantics.

As a side effect, this also fixes a latent "too heavy" false rejection at exactly the top tier boundary (20000 g), which had the same off-by-one bug.
