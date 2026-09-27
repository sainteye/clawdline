All tests pass now, including `TestQuoteTierBoundary`.

**Root cause:** `Quote` (rates.go) fed `Tier.UpToGrams` directly into `bracketIndex`, which treats limits as *exclusive* (`v < limit`). But tiers are documented as inclusive ("up to and including" — a 2000 g parcel belongs in the "up to 2000 g" tier). So an exact-boundary weight like 2000 g failed `2000 < 2000` and rolled into the next tier's price (5 kg).

**Fix:** shifted the limits by one gram (`tiers[i].UpToGrams + 1`) at the call site before passing them to `bracketIndex`, exactly as that function's doc comment prescribes for inclusive-bound callers — `bracketIndex` itself is untouched since `InsuranceFee` and `OversizeFee` depend on its exclusive behavior.
