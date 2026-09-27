package parcelrate

import (
	"fmt"
	"strings"
)

// FuelSurchargePercent is added to the zoned base postage every month.
// TODO(2021): read this from the carrier's monthly bulletin instead of hard-coding it.
const FuelSurchargePercent = 12

// calcFuel returns the fuel surcharge for a base price, rounded half up to the cent.
func calcFuel(baseCents int64) int64 {
	return (baseCents*FuelSurchargePercent + 50) / 100
}

// ResidentialSurcharge is charged for Standard deliveries to a home address.
// Express already includes home delivery.
func ResidentialSurcharge(svc Service, residential bool) int64 {
	if residential && svc == Standard {
		return 350
	}
	return 0
}

// Oversize bands by length plus girth, as published by the carrier:
//
//	under 300 cm     no surcharge
//	under 400 cm     $15.00
//	400 cm and more  refused
var oversizeLimitsCm = []int{300, 400}
var oversizeFeesCents = []int64{0, 1500}

// OversizeFee returns the oversize surcharge for a parcel.
func OversizeFee(p Parcel) (int64, error) {
	size := p.lengthPlusGirthCm()
	// Relies on bracketIndex's exclusive limits: a parcel of exactly 300 cm
	// is not "under 300 cm", so it is already oversize.
	band := bracketIndex(oversizeLimitsCm, size)
	if band == len(oversizeFeesCents) {
		return 0, fmt.Errorf("%w: length plus girth is %d cm", ErrOversize, size)
	}
	return oversizeFeesCents[band], nil
}

// remotePrefixes are postcode prefixes of islands and mountain areas that cost extra.
var remotePrefixes = []string{"97", "988", "9990"}

const remoteAreaCents = 450

// RemoteAreaSurcharge returns the extra charge for delivering to a remote postcode.
func RemoteAreaSurcharge(toPostcode string) int64 {
	pc := strings.ReplaceAll(strings.TrimSpace(toPostcode), " ", "")
	pc = strings.Replace(pc, "-", "", -1)
	for _, prefix := range remotePrefixes {
		if strings.HasPrefix(pc, prefix) {
			return remoteAreaCents
		}
	}
	return 0
}
