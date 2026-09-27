All tests pass, including the previously-failing `TestQuoteTierBoundary`.

**Cause**: `bracketIndex` (brackets.go:12) compared `v < limit`, treating tier limits as exclusive. But `Tier.UpToGrams` means "up to and including" (documented in rates.go:27-28, and used consistently elsewhere: letters.go:44, zones.go:70), so a 2000 g parcel should match the "up to 2000 g" tier, not get bumped into "up to 5000 g".

**Fix**: one-line change, `v < limit` → `v <= limit`, plus dropping the now-resolved TODO comment above it.

**Not changed** (flagging, not touching):
- `letters.go:42` has its own duplicate bracket-lookup loop with a `NOTE` saying it could reuse `bracketIndex` — worth revisiting since `bracketIndex` is now actually correct, but that's a separate refactor.
