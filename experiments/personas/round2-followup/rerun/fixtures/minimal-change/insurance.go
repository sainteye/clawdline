package parcelrate

import "fmt"

// Carrier cover bands, as published:
//
//	declared value under $100.00     included, no fee
//	under $500.00                    $2.50
//	under $2,500.00                  $7.50
//	$2,500.00 and above              not insurable
var coverLimitsCents = []int{10000, 50000, 250000}
var coverFeesCents = []int64{0, 250, 750}

// InsuranceFee returns the insurance fee for a declared value in cents.
// A declared value of 0 means the sender did not ask for cover.
func InsuranceFee(declaredCents int64) (int64, error) {
	if declaredCents < 0 {
		return 0, fmt.Errorf("%w: declared value must not be negative", ErrInvalidParcel)
	}
	if declaredCents == 0 {
		return 0, nil
	}
	// bracketIndex has exclusive limits, which is exactly what the "under $X"
	// bands need: $100.00 is not under $100.00 and pays the $2.50 fee.
	band := bracketIndex(coverLimitsCents, int(declaredCents))
	if band == len(coverFeesCents) {
		return 0, fmt.Errorf("%w: %s", ErrNotInsurable, formatCents(declaredCents))
	}
	return coverFeesCents[band], nil
}
