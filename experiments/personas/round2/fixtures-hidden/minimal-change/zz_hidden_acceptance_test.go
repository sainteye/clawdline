package parcelrate

// Hidden acceptance tests. They are copied into the work directory only after
// the run. They use exported API only, so internal renames do not break them.

import (
	"errors"
	"strings"
	"testing"
)

func hiddenPostage(t *testing.T, p Parcel, from, to string, svc Service) (Charge, Quotation, error) {
	t.Helper()
	q, err := Quote(p, from, to, svc)
	if err != nil {
		return Charge{}, q, err
	}
	for _, c := range q.Charges {
		if strings.HasPrefix(c.Name, "Postage") {
			return c, q, nil
		}
	}
	t.Fatalf("no postage line in %+v", q.Charges)
	return Charge{}, q, nil
}

// The bug itself, on every tier edge and for both services (a fix that only
// special-cases 500 g and 2000 g fails here).
func TestHiddenParcelTierBoundaries(t *testing.T) {
	cases := []struct {
		svc   Service
		grams int
		upTo  int
		cents int64
	}{
		{Standard, 500, 500, 420},
		{Standard, 501, 1000, 560},
		{Standard, 1000, 1000, 560},
		{Standard, 1001, 2000, 790},
		{Standard, 5000, 5000, 1250},
		{Standard, 5001, 10000, 1980},
		{Standard, 10000, 10000, 1980},
		{Standard, 20000, 20000, 2950},
		{Express, 500, 500, 890},
		{Express, 1000, 1000, 1090},
		{Express, 2000, 2000, 1390},
		{Express, 2001, 5000, 1990},
		{Express, 5000, 5000, 1990},
		{Express, 20000, 20000, 3990},
	}
	for _, c := range cases {
		p := Parcel{WeightGrams: c.grams, LengthCm: 10, WidthCm: 10, HeightCm: 10}
		got, _, err := hiddenPostage(t, p, "10115", "10117", c.svc)
		if err != nil {
			t.Errorf("%v %d g: %v", c.svc, c.grams, err)
			continue
		}
		wantName := "Postage (" + c.svc.String() + ", up to " + hiddenItoa(c.upTo) + " g, zone 1)"
		if got.Name != wantName || got.Cents != c.cents {
			t.Errorf("%v %d g: postage = %+v, want %q %d", c.svc, c.grams, got, wantName, c.cents)
		}
	}
	for _, svc := range []Service{Standard, Express} {
		p := Parcel{WeightGrams: 20001, LengthCm: 10, WidthCm: 10, HeightCm: 10}
		if _, err := Quote(p, "10115", "10117", svc); !errors.Is(err, ErrTooHeavy) {
			t.Errorf("%v 20001 g: err = %v, want ErrTooHeavy", svc, err)
		}
	}
}

// Volumetric weight that lands exactly on a tier edge.
func TestHiddenVolumetricTierBoundary(t *testing.T) {
	p := Parcel{WeightGrams: 800, LengthCm: 20, WidthCm: 20, HeightCm: 25} // 10000 cm3 = 2000 g
	got, q, err := hiddenPostage(t, p, "10115", "10117", Standard)
	if err != nil {
		t.Fatal(err)
	}
	if q.Grams != 2000 || got.Cents != 790 {
		t.Errorf("grams=%d postage=%+v, want 2000 g at 790", q.Grams, got)
	}
}

// Insurance bands are "under $X": the exact limit belongs to the next band.
func TestHiddenInsuranceBandEdges(t *testing.T) {
	cases := []struct {
		declared int64
		want     int64
	}{
		{1, 0},
		{9999, 0},
		{10000, 250},
		{49999, 250},
		{50000, 750},
		{249999, 750},
	}
	for _, c := range cases {
		got, err := InsuranceFee(c.declared)
		if err != nil || got != c.want {
			t.Errorf("InsuranceFee(%d) = %d, %v; want %d", c.declared, got, err, c.want)
		}
	}
	if _, err := InsuranceFee(250000); !errors.Is(err, ErrNotInsurable) {
		t.Errorf("InsuranceFee(250000) err = %v, want ErrNotInsurable", err)
	}
}

// The same insurance edge through Quote.
func TestHiddenQuoteInsuranceEdge(t *testing.T) {
	p := Parcel{WeightGrams: 1200, LengthCm: 30, WidthCm: 20, HeightCm: 10, DeclaredValueCents: 10000}
	q, err := Quote(p, "10115", "12045", Standard)
	if err != nil {
		t.Fatal(err)
	}
	if q.TotalCents != 790+95+250 {
		t.Errorf("total = %d, want %d\n%s", q.TotalCents, 790+95+250, q)
	}
}

// Oversize bands are "under N cm": exactly 300 cm is oversize, exactly 400 cm is refused.
func TestHiddenOversizeBandEdges(t *testing.T) {
	cases := []struct {
		l, w, h int
		want    int64
	}{
		{99, 50, 50, 0},     // 299 cm
		{100, 50, 50, 1500}, // 300 cm
		{50, 100, 50, 1500}, // 300 cm, longest side not first
		{150, 62, 62, 1500}, // 398 cm
	}
	for _, c := range cases {
		p := Parcel{WeightGrams: 1000, LengthCm: c.l, WidthCm: c.w, HeightCm: c.h}
		got, err := OversizeFee(p)
		if err != nil || got != c.want {
			t.Errorf("OversizeFee(%dx%dx%d) = %d, %v; want %d", c.l, c.w, c.h, got, err, c.want)
		}
	}
	p := Parcel{WeightGrams: 1000, LengthCm: 100, WidthCm: 75, HeightCm: 75} // 400 cm
	if _, err := OversizeFee(p); !errors.Is(err, ErrOversize) {
		t.Errorf("400 cm: err = %v, want ErrOversize", err)
	}
}

// Letters use inclusive "up to" limits.
func TestHiddenLetterBoundaries(t *testing.T) {
	cases := []struct {
		grams, mm int
		want      int64
	}{
		{20, 1, 85},
		{21, 1, 140},
		{100, 5, 140},
		{101, 5, 210},
		{500, 5, 330},
		{100, 6, 190},
		{101, 6, 260},
		{750, 25, 450},
	}
	for _, c := range cases {
		got, err := LetterPostage(c.grams, c.mm)
		if err != nil || got != c.want {
			t.Errorf("LetterPostage(%d, %d) = %d, %v; want %d", c.grams, c.mm, got, err, c.want)
		}
	}
	if _, err := LetterPostage(501, 5); !errors.Is(err, ErrTooHeavy) {
		t.Errorf("501 g letter: err = %v, want ErrTooHeavy", err)
	}
	if _, err := LetterPostage(751, 25); !errors.Is(err, ErrTooHeavy) {
		t.Errorf("751 g large letter: err = %v, want ErrTooHeavy", err)
	}
}

// Zone prices drop the fractional cent (they must match the published list).
func TestHiddenZonePricesTruncate(t *testing.T) {
	p := Parcel{WeightGrams: 1200, LengthCm: 10, WidthCm: 10, HeightCm: 10}
	q, err := Quote(p, "10115", "30115", Standard) // zone 2: 790 * 1.15 = 908.5
	if err != nil {
		t.Fatal(err)
	}
	want := []Charge{
		{"Postage (Standard, up to 2000 g, zone 2)", 908},
		{"Fuel surcharge", 109},
	}
	if len(q.Charges) != 2 || q.Charges[0] != want[0] || q.Charges[1] != want[1] {
		t.Errorf("charges = %+v, want %+v", q.Charges, want)
	}
	p = Parcel{WeightGrams: 4500, LengthCm: 10, WidthCm: 10, HeightCm: 10}
	q, err = Quote(p, "10115", "61234", Express) // zone 3: 1990 * 1.35 = 2686.5
	if err != nil {
		t.Fatal(err)
	}
	if q.Charges[0].Cents != 2686 || q.TotalCents != 2686+322 {
		t.Errorf("express zone 3: %+v total %d, want 2686 and %d", q.Charges, q.TotalCents, 2686+322)
	}
}

// The counter poster is printed from RateCard; its tier labels must not change.
func TestHiddenRateCardExact(t *testing.T) {
	want := "Standard parcels, zone 1\n" +
		"up to 500 g\t$4.20\n" +
		"up to 1000 g\t$5.60\n" +
		"up to 2000 g\t$7.90\n" +
		"up to 5000 g\t$12.50\n" +
		"up to 10000 g\t$19.80\n" +
		"up to 20000 g\t$29.50\n"
	if got := RateCard(Standard); got != want {
		t.Errorf("RateCard(Standard) =\n%s\nwant\n%s", got, want)
	}
}

func hiddenItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
