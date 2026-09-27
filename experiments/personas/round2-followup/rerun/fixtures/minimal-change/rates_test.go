package parcelrate

import (
	"errors"
	"strings"
	"testing"
)

func chargeNamed(q Quotation, prefix string) (Charge, bool) {
	for _, c := range q.Charges {
		if strings.HasPrefix(c.Name, prefix) {
			return c, true
		}
	}
	return Charge{}, false
}

func TestQuoteLocalStandard(t *testing.T) {
	p := Parcel{WeightGrams: 1200, LengthCm: 30, WidthCm: 20, HeightCm: 10}
	q, err := Quote(p, "10115", "12045", Standard)
	if err != nil {
		t.Fatal(err)
	}
	if q.Zone != 1 || q.Grams != 1200 {
		t.Errorf("zone=%d grams=%d, want zone 1, 1200 g", q.Zone, q.Grams)
	}
	want := []Charge{
		{"Postage (Standard, up to 2000 g, zone 1)", 790},
		{"Fuel surcharge", 95},
	}
	if len(q.Charges) != len(want) {
		t.Fatalf("charges = %+v, want %+v", q.Charges, want)
	}
	for i := range want {
		if q.Charges[i] != want[i] {
			t.Errorf("charge %d = %+v, want %+v", i, q.Charges[i], want[i])
		}
	}
	if q.TotalCents != 885 {
		t.Errorf("total = %d, want 885", q.TotalCents)
	}
}

func TestQuoteExpressInsuredFarZone(t *testing.T) {
	// 40x30x20 cm is 4800 g volumetric, heavier than the 3300 g actual weight.
	p := Parcel{WeightGrams: 3300, LengthCm: 40, WidthCm: 30, HeightCm: 20, DeclaredValueCents: 20000, Residential: true}
	q, err := Quote(p, "10115", "81234", Express)
	if err != nil {
		t.Fatal(err)
	}
	if q.Grams != 4800 || q.Zone != 4 {
		t.Errorf("grams=%d zone=%d, want 4800 g, zone 4", q.Grams, q.Zone)
	}
	if c, _ := chargeNamed(q, "Postage"); c.Cents != 3184 {
		t.Errorf("postage = %+v, want 3184", c)
	}
	if _, ok := chargeNamed(q, "Residential"); ok {
		t.Errorf("express must not charge residential delivery: %+v", q.Charges)
	}
	if c, _ := chargeNamed(q, "Insurance"); c.Cents != 250 {
		t.Errorf("insurance = %+v, want 250", c)
	}
	if q.TotalCents != 3184+382+250 {
		t.Errorf("total = %d, want %d", q.TotalCents, 3184+382+250)
	}
}

func TestQuoteRemoteResidential(t *testing.T) {
	p := Parcel{WeightGrams: 700, LengthCm: 20, WidthCm: 15, HeightCm: 10, Residential: true}
	q, err := Quote(p, "10115", "97420", Standard)
	if err != nil {
		t.Fatal(err)
	}
	if q.TotalCents != 896+108+350+450 {
		t.Errorf("total = %d, want %d\n%s", q.TotalCents, 896+108+350+450, q)
	}
}

// Parcels that weigh exactly a tier's limit belong to that tier:
// the price list says "up to 2000 g".
func TestQuoteTierBoundary(t *testing.T) {
	cases := []struct {
		grams    int
		postage  int64
		tierName string
	}{
		{500, 420, "Postage (Standard, up to 500 g, zone 1)"},
		{2000, 790, "Postage (Standard, up to 2000 g, zone 1)"},
	}
	for _, c := range cases {
		p := Parcel{WeightGrams: c.grams, LengthCm: 10, WidthCm: 10, HeightCm: 10}
		q, err := Quote(p, "10115", "10117", Standard)
		if err != nil {
			t.Fatalf("%d g: %v", c.grams, err)
		}
		got := q.Charges[0]
		if got.Name != c.tierName || got.Cents != c.postage {
			t.Errorf("%d g: postage = %+v, want %q %d", c.grams, got, c.tierName, c.postage)
		}
	}
}

func TestQuoteTooHeavy(t *testing.T) {
	p := Parcel{WeightGrams: 25000, LengthCm: 40, WidthCm: 40, HeightCm: 40}
	if _, err := Quote(p, "10115", "10117", Standard); !errors.Is(err, ErrTooHeavy) {
		t.Errorf("err = %v, want ErrTooHeavy", err)
	}
}

func TestQuoteInvalidInput(t *testing.T) {
	ok := Parcel{WeightGrams: 1000, LengthCm: 10, WidthCm: 10, HeightCm: 10}
	if _, err := Quote(ok, "1011", "10117", Standard); err == nil {
		t.Error("want an error for a 4-digit postcode")
	}
	if _, err := Quote(Parcel{LengthCm: 1, WidthCm: 1, HeightCm: 1}, "10115", "10117", Standard); !errors.Is(err, ErrInvalidParcel) {
		t.Errorf("err = %v, want ErrInvalidParcel", err)
	}
}

func TestCheapestServicePrefersStandard(t *testing.T) {
	p := Parcel{WeightGrams: 1200, LengthCm: 30, WidthCm: 20, HeightCm: 10}
	q, err := CheapestService(p, "10115", "12045")
	if err != nil {
		t.Fatal(err)
	}
	if q.Service != Standard {
		t.Errorf("service = %v, want Standard", q.Service)
	}
}

func TestBillableGrams(t *testing.T) {
	cases := []struct {
		p    Parcel
		want int
	}{
		{Parcel{WeightGrams: 1234, LengthCm: 10, WidthCm: 10, HeightCm: 10}, 1300},
		{Parcel{WeightGrams: 2000, LengthCm: 50, WidthCm: 40, HeightCm: 30}, 12000},
	}
	for _, c := range cases {
		if got := c.p.BillableGrams(); got != c.want {
			t.Errorf("BillableGrams(%+v) = %d, want %d", c.p, got, c.want)
		}
	}
}

func TestRateCardHeader(t *testing.T) {
	card := RateCard(Express)
	if !strings.HasPrefix(card, "Express parcels, zone 1\n") {
		t.Errorf("card = %q", card)
	}
	if n := strings.Count(card, "\n"); n != 7 {
		t.Errorf("card has %d lines, want 7", n)
	}
}
