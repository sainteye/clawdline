package parcelrate

import "testing"

func TestBracketIndex(t *testing.T) {
	limits := []int{100, 500, 1000}
	cases := []struct{ v, want int }{
		{0, 0},
		{42, 0},
		{250, 1},
		{700, 2},
		{5000, 3},
	}
	for _, c := range cases {
		if got := bracketIndex(limits, c.v); got != c.want {
			t.Errorf("bracketIndex(%v, %d) = %d, want %d", limits, c.v, got, c.want)
		}
	}
	if got := bracketIndex(nil, 1); got != 0 {
		t.Errorf("bracketIndex(nil, 1) = %d, want 0", got)
	}
}

func TestClampInt(t *testing.T) {
	if clampInt(-3, 0, 9) != 0 || clampInt(12, 0, 9) != 9 || clampInt(4, 0, 9) != 4 {
		t.Error("clampInt does not clamp")
	}
}
