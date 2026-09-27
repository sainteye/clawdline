package parcelrate

import (
	"fmt"
	"strings"
)

// Zone is the pricing zone between two postcodes. Zone 1 is local delivery
// within the same region; zone 4 is the far end of the country.
type Zone int

// Postcodes are five digits. The first digit is the region (0-9).

// zoneTable maps the distance between the sender's and the recipient's region
// to a zone. A distance is in the first row whose MaxDistance is >= it.
var zoneTable = []struct {
	MaxDistance int
	Zone        Zone
}{
	{0, 1},
	{2, 2},
	{5, 3},
	{9, 4},
}

// zoneFactorPercent scales the base postage by zone.
var zoneFactorPercent = map[Zone]int64{
	1: 100,
	2: 115,
	3: 135,
	4: 160,
}

// normalizePostcode removes spaces and dashes and checks the postcode is five digits.
func normalizePostcode(pc string) (string, error) {
	pc = strings.TrimSpace(pc)
	pc = strings.ReplaceAll(pc, " ", "")
	pc = strings.ReplaceAll(pc, "-", "")
	if len(pc) != 5 {
		return "", fmt.Errorf("parcelrate: postcode %q must have 5 digits", pc)
	}
	for _, r := range pc {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("parcelrate: postcode %q must have 5 digits", pc)
		}
	}
	return pc, nil
}

// ZoneBetween returns the pricing zone for a parcel sent from one postcode to another.
func ZoneBetween(from, to string) (Zone, error) {
	f, err := normalizePostcode(from)
	if err != nil {
		return 0, err
	}
	t, err := normalizePostcode(to)
	if err != nil {
		return 0, err
	}
	distance := int(f[0]) - int(t[0])
	if distance < 0 {
		distance = -distance
	}
	distance = clampInt(distance, 0, 9)

	var zone Zone
	found := false
	for i := 0; i < len(zoneTable); i++ {
		if found == false {
			if distance <= zoneTable[i].MaxDistance {
				zone = zoneTable[i].Zone
				found = true
			}
		}
	}
	if !found {
		// cannot happen: distance is clamped to 0-9
		zone = 4
	}
	return zone, nil
}

// applyZoneFactor scales a base price by the zone factor.
//
// It truncates (rounds down) on purpose: the carrier's published zone prices
// are the zone-1 price times the factor with the fractional cent dropped, and
// our quotes must match the published price list to the cent.
func applyZoneFactor(cents int64, zone Zone) int64 {
	return cents * zoneFactorPercent[zone] / 100
}
