All tests pass, including the pre-existing negative-amount cases in `money_test.go` that were presumably failing before.

The bug: Go's `%` operator keeps the sign of the dividend, so `-1250 % 100` is `-50`, not `50`. `FormatCents` applied `/` and `%` directly to negative cents, so both the dollar and cent parts came out negative, printed as `$-12.-50`. Fixed by taking the absolute value once and prepending a single `-` before the `$`.
