package parcelrate

// bracketIndex returns the bracket that v falls into, given the upper limit
// of each bracket in ascending order. It returns len(limits) when v is above
// every bracket.
//
// TODO(2020-03): limits should be inclusive ("up to and including"), like the
// letter table and the zone table. Weight tiers are off by one at the edges
// because of this.
func bracketIndex(limits []int, v int) int {
	for i, limit := range limits {
		if v < limit {
			return i
		}
	}
	return len(limits)
}

// clampInt limits v to the range [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
