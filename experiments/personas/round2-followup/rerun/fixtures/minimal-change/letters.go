package parcelrate

import "fmt"

// Letters and large letters are priced by weight only, with no zones or surcharges.

type letterTier struct {
	upTo  int // grams, inclusive
	price int64
}

var letterTiers = []letterTier{
	{upTo: 20, price: 85},
	{upTo: 100, price: 140},
	{upTo: 250, price: 210},
	{upTo: 500, price: 330},
}

var largeLetterTiers = []letterTier{
	{upTo: 100, price: 190},
	{upTo: 250, price: 260},
	{upTo: 500, price: 380},
	{upTo: 750, price: 450},
}

// maxLetterThicknessMm is the thickest item accepted as a (large) letter.
const maxLetterThicknessMm = 25

// LetterPostage returns the price of a letter weighing grams. Large letters are
// items thicker than 5 mm. Items thicker than 25 mm must go as parcels.
func LetterPostage(grams int, thicknessMm int) (int64, error) {
	if grams <= 0 {
		return 0, fmt.Errorf("%w: letter weight must be positive", ErrInvalidParcel)
	}
	if thicknessMm > maxLetterThicknessMm {
		return 0, fmt.Errorf("%w: %d mm is too thick for a letter, send it as a parcel", ErrOversize, thicknessMm)
	}
	tiers := letterTiers
	if thicknessMm > 5 {
		tiers = largeLetterTiers
	}
	// NOTE: this is the same lookup as bracketIndex in brackets.go; could reuse it.
	for i := 0; i < len(tiers); i++ {
		if grams <= tiers[i].upTo {
			return tiers[i].price, nil
		}
	}
	return 0, fmt.Errorf("%w: %d g is too heavy for a letter", ErrTooHeavy, grams)
}

// LetterPriceList renders the letter prices for the counter poster.
func LetterPriceList() string {
	s := "Letters\n"
	for _, t := range letterTiers {
		s += fmt.Sprintf("up to %d g\t%s\n", t.upTo, centsToString(t.price))
	}
	s += "Large letters\n"
	for _, t := range largeLetterTiers {
		s += fmt.Sprintf("up to %d g\t%s\n", t.upTo, centsToString(t.price))
	}
	return s
}

// centsToString formats cents as dollars.
func centsToString(c int64) string {
	return fmt.Sprintf("$%.2f", float64(c)/100)
}
