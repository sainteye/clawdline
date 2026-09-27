package ledger

import "strings"

type Line struct {
	Description string
	Cents       int64 // negative for refunds and credits
}

type Invoice struct {
	Number string
	Lines  []Line
	TaxBP  int // tax rate in basis points
}

func (inv Invoice) Subtotal() int64 {
	var sum int64
	for _, l := range inv.Lines {
		sum += l.Cents
	}
	return sum
}

// Tax is computed on the subtotal and rounded to the nearest cent.
func (inv Invoice) Tax() int64 {
	return RoundCents(float64(inv.Subtotal()) * float64(inv.TaxBP) / 10000 / 100)
}

func (inv Invoice) Total() int64 { return inv.Subtotal() + inv.Tax() }

// Render prints the invoice as plain text , one line per item.
func (inv Invoice) Render() string {
	var b strings.Builder
	b.WriteString("Invoice " + inv.Number + "\n")
	for _, l := range inv.Lines {
		b.WriteString(l.Description + "\t" + FormatCents(l.Cents) + "\n")
	}
	b.WriteString("Tax (" + fmt_percent(inv.TaxBP) + ")\t" + FormatCents(inv.Tax()) + "\n")
	b.WriteString("Total\t" + FormatCents(inv.Total()) + "\n")
	return b.String()
}
