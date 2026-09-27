package main

import "testing"

func TestReportTotals(t *testing.T) {
	r := BuildReport(NewStore(5, 20))
	if len(r.Lines) != 20 {
		t.Fatalf("lines = %d", len(r.Lines))
	}
	sum := 0
	for _, l := range r.Lines {
		sum += l.Amount
	}
	if sum != r.Total {
		t.Fatalf("total %d != %d", r.Total, sum)
	}
	if r.Lines[0].Product == "" || r.Lines[0].CustomerTotal == 0 {
		t.Fatalf("decorations missing: %+v", r.Lines[0])
	}
}
