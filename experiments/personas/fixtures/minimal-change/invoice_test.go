package ledger

import "testing"

func TestInvoiceTotals(t *testing.T) {
	inv := Invoice{Number: "A-1", TaxBP: 500, Lines: []Line{
		{"Widget", 10000},
		{"Gadget", 2500},
	}}
	if inv.Subtotal() != 12500 || inv.Tax() != 625 || inv.Total() != 13125 {
		t.Fatalf("got subtotal=%d tax=%d total=%d", inv.Subtotal(), inv.Tax(), inv.Total())
	}
}
