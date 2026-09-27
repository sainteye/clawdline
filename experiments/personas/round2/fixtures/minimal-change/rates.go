package parcelrate

import (
	"fmt"
	"strconv"
	"strings"
)

// Service is a delivery speed.
type Service int

const (
	Standard Service = iota
	Express
)

func (s Service) String() string {
	switch s {
	case Standard:
		return "Standard"
	case Express:
		return "Express"
	}
	return "Service(" + strconv.Itoa(int(s)) + ")"
}

// Tier is one row of a weight price list: parcels weighing up to and
// including UpToGrams cost Cents (zone 1 price, before surcharges).
type Tier struct {
	UpToGrams int
	Cents     int64
}

var standardTiers = []Tier{
	{UpToGrams: 500, Cents: 420},
	{UpToGrams: 1000, Cents: 560},
	{UpToGrams: 2000, Cents: 790},
	{UpToGrams: 5000, Cents: 1250},
	{UpToGrams: 10000, Cents: 1980},
	{UpToGrams: 20000, Cents: 2950},
}

// TODO(2021): express prices should come from the carrier API like the fuel surcharge.
var expressTiers = []Tier{
	{UpToGrams: 500, Cents: 890},
	{UpToGrams: 1000, Cents: 1090},
	{UpToGrams: 2000, Cents: 1390},
	{UpToGrams: 5000, Cents: 1990},
	{UpToGrams: 10000, Cents: 2890},
	{UpToGrams: 20000, Cents: 3990},
}

// Charge is one line of a quotation.
type Charge struct {
	Name  string
	Cents int64
}

// Quotation is the price of sending one parcel with one service.
type Quotation struct {
	Service    Service
	Zone       Zone
	Grams      int // billable weight
	Charges    []Charge
	TotalCents int64
}

// Quote prices a parcel sent from one postcode to another with the given service.
func Quote(p Parcel, fromPostcode, toPostcode string, svc Service) (Quotation, error) {
	if err := p.Validate(); err != nil {
		return Quotation{}, err
	}
	var tiers []Tier
	if svc == Standard {
		tiers = standardTiers
	} else if svc == Express {
		tiers = expressTiers
	} else {
		return Quotation{}, fmt.Errorf("parcelrate: unknown service %v", svc)
	}

	zone, err := ZoneBetween(fromPostcode, toPostcode)
	if err != nil {
		return Quotation{}, err
	}

	// find the weight tier (see the TODO on bracketIndex about tier edges)
	grams := p.BillableGrams()
	limits := make([]int, len(tiers))
	for i := range tiers {
		limits[i] = tiers[i].UpToGrams
	}
	idx := bracketIndex(limits, grams)
	if idx == len(tiers) {
		return Quotation{}, fmt.Errorf("%w: %d g billable, %s limit is %d g",
			ErrTooHeavy, grams, svc, tiers[len(tiers)-1].UpToGrams)
	}
	tier := tiers[idx]

	q := Quotation{Service: svc, Zone: zone, Grams: grams}

	// base postage for the zone
	base := applyZoneFactor(tier.Cents, zone)
	q.Charges = append(q.Charges, Charge{
		Name:  fmt.Sprintf("Postage (%s, up to %d g, zone %d)", svc, tier.UpToGrams, zone),
		Cents: base,
	})

	// fuel
	fuel := calcFuel(base)
	if fuel > 0 {
		q.Charges = append(q.Charges, Charge{Name: "Fuel surcharge", Cents: fuel})
	}

	// residential
	if r := ResidentialSurcharge(svc, p.Residential); r > 0 {
		q.Charges = append(q.Charges, Charge{Name: "Residential delivery", Cents: r})
	}

	// oversize
	over, err := OversizeFee(p)
	if err != nil {
		return Quotation{}, err
	}
	if over > 0 {
		q.Charges = append(q.Charges, Charge{Name: "Oversize", Cents: over})
	}

	// remote area
	remote := RemoteAreaSurcharge(toPostcode)
	if remote != 0 {
		q.Charges = append(q.Charges, Charge{Name: "Remote area", Cents: remote})
	}

	// insurance
	if p.DeclaredValueCents > 0 {
		ins, err := InsuranceFee(p.DeclaredValueCents)
		if err != nil {
			return Quotation{}, err
		}
		q.Charges = append(q.Charges, Charge{
			Name:  "Insurance (declared " + formatCents(p.DeclaredValueCents) + ")",
			Cents: ins,
		})
	}

	// total
	var total int64
	for i := 0; i < len(q.Charges); i++ {
		total = total + q.Charges[i].Cents
	}
	q.TotalCents = total
	return q, nil
}

// CheapestService returns the cheaper of the Standard and Express quotes.
// Express wins only if it is strictly cheaper.
func CheapestService(p Parcel, fromPostcode, toPostcode string) (Quotation, error) {
	std, err := Quote(p, fromPostcode, toPostcode, Standard)
	if err != nil {
		return Quotation{}, err
	}
	exp, err := Quote(p, fromPostcode, toPostcode, Express)
	if err != nil {
		return std, nil
	}
	if exp.TotalCents < std.TotalCents {
		return exp, nil
	}
	return std, nil
}

// String renders the quotation as a plain-text receipt.
func (q Quotation) String() string {
	var b strings.Builder
	for _, c := range q.Charges {
		b.WriteString(fmt.Sprintf("%-40s %10s\n", c.Name, formatCents(c.Cents)))
	}
	b.WriteString(fmt.Sprintf("%-40s %10s\n", "Total", formatCents(q.TotalCents)))
	return b.String()
}

// RateCard renders the zone-1 price list for a service, one tier per line,
// in the format printed on the counter poster.
func RateCard(svc Service) string {
	tiers := standardTiers
	if svc == Express {
		tiers = expressTiers
	}
	var b strings.Builder
	b.WriteString(svc.String() + " parcels, zone 1\n")
	for _, t := range tiers {
		b.WriteString("up to " + strconv.Itoa(t.UpToGrams) + " g\t" + formatCents(t.Cents) + "\n")
	}
	return b.String()
}

// formatCents renders a non-negative amount of cents as dollars, e.g. 1250 -> "$12.50".
func formatCents(c int64) string {
	return "$" + strconv.FormatInt(c/100, 10) + "." + fmt.Sprintf("%02d", c%100)
}
