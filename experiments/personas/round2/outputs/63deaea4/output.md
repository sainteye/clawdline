All tests pass, including `TestQuoteTierBoundary` which specifically checks the 2000g edge case.

**Root cause:** `bracketIndex` (brackets.go) uses exclusive limits (`v < limit`) — correct for the oversize and insurance bands ("under $X"), but the weight tiers in `rates.go` are documented as inclusive ("up to and including UpToGrams"). A 2kg (2000g) parcel failed `2000 < 2000`, fell through to the next bracket, and got priced at the 5kg tier.

**Fix:** in `rates.go`, shift the tier limits up by 1 (`UpToGrams + 1`) before calling `bracketIndex`, converting them from exclusive to inclusive at that one call site — without touching `bracketIndex`'s shared exclusive-limit contract, which `surcharges.go` and `insurance.go` correctly depend on. Also cleaned up the now-resolved TODO comment.
