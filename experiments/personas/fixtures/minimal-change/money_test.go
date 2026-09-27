package ledger

import "testing"

func TestFormatCents(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "$0.00"},
		{5, "$0.05"},
		{123456, "$1,234.56"},
		{100000000, "$1,000,000.00"},
		{-1250, "-$12.50"},
		{-50, "-$0.50"},
		{-123456, "-$1,234.56"},
	}
	for _, c := range cases {
		if got := FormatCents(c.in); got != c.want {
			t.Errorf("FormatCents(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRoundCents(t *testing.T) {
	if got := RoundCents(12.345); got != 1235 {
		t.Errorf("RoundCents(12.345) = %d, want 1235", got)
	}
	if got := RoundCents(-12.345); got != -1235 {
		t.Errorf("RoundCents(-12.345) = %d, want -1235", got)
	}
}

func TestFmtPercent(t *testing.T) {
	if got := fmt_percent(1250); got != "12.50%" {
		t.Errorf("fmt_percent(1250) = %q", got)
	}
}
