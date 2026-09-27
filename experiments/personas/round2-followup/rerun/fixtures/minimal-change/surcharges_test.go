package parcelrate

import (
	"errors"
	"testing"
)

func TestInsuranceFee(t *testing.T) {
	cases := []struct {
		declared int64
		want     int64
	}{
		{0, 0},
		{5000, 0},
		{20000, 250},
		{100000, 750},
	}
	for _, c := range cases {
		got, err := InsuranceFee(c.declared)
		if err != nil || got != c.want {
			t.Errorf("InsuranceFee(%d) = %d, %v; want %d", c.declared, got, err, c.want)
		}
	}
	if _, err := InsuranceFee(300000); !errors.Is(err, ErrNotInsurable) {
		t.Errorf("InsuranceFee(300000) err = %v, want ErrNotInsurable", err)
	}
	if _, err := InsuranceFee(-1); !errors.Is(err, ErrInvalidParcel) {
		t.Errorf("InsuranceFee(-1) err = %v, want ErrInvalidParcel", err)
	}
}

func TestOversizeFee(t *testing.T) {
	small := Parcel{WeightGrams: 1000, LengthCm: 50, WidthCm: 40, HeightCm: 30} // 190 cm
	if fee, err := OversizeFee(small); err != nil || fee != 0 {
		t.Errorf("small: %d, %v", fee, err)
	}
	long := Parcel{WeightGrams: 1000, LengthCm: 40, WidthCm: 150, HeightCm: 50} // 330 cm
	if fee, err := OversizeFee(long); err != nil || fee != 1500 {
		t.Errorf("long: %d, %v; want 1500", fee, err)
	}
	huge := Parcel{WeightGrams: 1000, LengthCm: 200, WidthCm: 60, HeightCm: 50} // 420 cm
	if _, err := OversizeFee(huge); !errors.Is(err, ErrOversize) {
		t.Errorf("huge: err = %v, want ErrOversize", err)
	}
}

func TestRemoteAreaSurcharge(t *testing.T) {
	cases := map[string]int64{
		"97420":  450,
		"988 01": 450,
		"99905":  450,
		"99801":  0,
		"10115":  0,
	}
	for pc, want := range cases {
		if got := RemoteAreaSurcharge(pc); got != want {
			t.Errorf("RemoteAreaSurcharge(%q) = %d, want %d", pc, got, want)
		}
	}
}

func TestZoneBetween(t *testing.T) {
	cases := []struct {
		from, to string
		want     Zone
	}{
		{"10115", "12345", 1},
		{"10115", "30115", 2},
		{"10115", "61234", 3},
		{"01234", "98765", 4},
		{"981 23", "10-115", 4},
	}
	for _, c := range cases {
		got, err := ZoneBetween(c.from, c.to)
		if err != nil || got != c.want {
			t.Errorf("ZoneBetween(%q, %q) = %d, %v; want %d", c.from, c.to, got, err, c.want)
		}
	}
	if _, err := ZoneBetween("ABCDE", "10115"); err == nil {
		t.Error("want an error for a non-numeric postcode")
	}
}

func TestLetterPostage(t *testing.T) {
	cases := []struct {
		grams, mm int
		want      int64
	}{
		{15, 3, 85},
		{60, 3, 140},
		{300, 10, 380},
	}
	for _, c := range cases {
		got, err := LetterPostage(c.grams, c.mm)
		if err != nil || got != c.want {
			t.Errorf("LetterPostage(%d, %d) = %d, %v; want %d", c.grams, c.mm, got, err, c.want)
		}
	}
	if _, err := LetterPostage(800, 10); !errors.Is(err, ErrTooHeavy) {
		t.Errorf("800 g large letter: err = %v, want ErrTooHeavy", err)
	}
	if _, err := LetterPostage(50, 30); !errors.Is(err, ErrOversize) {
		t.Errorf("30 mm letter: err = %v, want ErrOversize", err)
	}
}
