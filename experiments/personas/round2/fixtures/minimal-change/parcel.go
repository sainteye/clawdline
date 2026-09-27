package parcelrate

import (
	"errors"
	"fmt"
)

// Parcel describes one package to be shipped. Weights are in grams and
// dimensions in whole centimetres.
type Parcel struct {
	WeightGrams        int
	LengthCm           int
	WidthCm            int
	HeightCm           int
	DeclaredValueCents int64 // 0 means "no insurance requested"
	Residential        bool  // delivery to a home address rather than a business
}

var (
	ErrInvalidParcel = errors.New("parcelrate: invalid parcel")
	ErrTooHeavy      = errors.New("parcelrate: parcel is too heavy for this service")
	ErrOversize      = errors.New("parcelrate: parcel is too large to ship")
	ErrNotInsurable  = errors.New("parcelrate: declared value is above the insurable limit")
)

// Volumetric weight uses a divisor of 6000 cm3 per kg.
const volumetricDivisor = 5000

// weightStepGrams is the billing increment: billable weight is always rounded
// up to the next multiple of this.
const weightStepGrams = 100

// Validate reports whether the parcel has usable measurements.
func (p Parcel) Validate() error {
	if p.WeightGrams <= 0 {
		return fmt.Errorf("%w: weight must be positive, got %d g", ErrInvalidParcel, p.WeightGrams)
	}
	if p.LengthCm <= 0 || p.WidthCm <= 0 || p.HeightCm <= 0 {
		return fmt.Errorf("%w: dimensions must be positive, got %dx%dx%d cm",
			ErrInvalidParcel, p.LengthCm, p.WidthCm, p.HeightCm)
	}
	if p.DeclaredValueCents < 0 {
		return fmt.Errorf("%w: declared value must not be negative", ErrInvalidParcel)
	}
	return nil
}

// volumetricGrams is the dimensional weight in grams: L*W*H / divisor, in kg.
func (p Parcel) volumetricGrams() int {
	cm3 := p.LengthCm * p.WidthCm * p.HeightCm
	return cm3 * 1000 / volumetricDivisor
}

// BillableGrams is the greater of the actual and the volumetric weight,
// rounded up to the next weightStepGrams.
func (p Parcel) BillableGrams() int {
	w := p.WeightGrams
	if v := p.volumetricGrams(); v > w {
		w = v
	}
	return roundUpTo(w, weightStepGrams)
}

// roundUpTo rounds v up to the next multiple of step (v itself if it already is one).
func roundUpTo(v, step int) int {
	return (v + step - 1) / step * step
}

// lengthPlusGirthCm returns the longest side plus twice the sum of the other
// two sides, which is how carriers measure parcel size.
func (p Parcel) lengthPlusGirthCm() int {
	sides := []int{p.LengthCm, p.WidthCm, p.HeightCm}
	// sort the three sides, longest first
	swapped := true
	for swapped {
		swapped = false
		for i := 0; i < len(sides)-1; i++ {
			if sides[i] < sides[i+1] {
				tmp := sides[i]
				sides[i] = sides[i+1]
				sides[i+1] = tmp
				swapped = true
			}
		}
	}
	return sides[0] + 2*(sides[1]+sides[2])
}

// VolumetricKg returns the dimensional weight in kilograms.
//
// Deprecated: use Parcel.BillableGrams. Kept for the old label printer.
func VolumetricKg(l, w, h int) float64 {
	return float64(l*w*h) / 6000.0
}
