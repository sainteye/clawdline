package main

import (
	"fmt"
	"strconv"
)

// currencyInfo describes how a currency's minor units are written.
type currencyInfo struct {
	Code     string
	Exponent int // number of minor-unit digits, e.g. 2 for USD
}

var currencies = map[string]currencyInfo{}

func init() {
	for _, c := range []currencyInfo{
		{Code: "USD", Exponent: 2},
		{Code: "EUR", Exponent: 2},
		{Code: "GBP", Exponent: 2},
		{Code: "CAD", Exponent: 2},
		{Code: "JPY", Exponent: 0},
		{Code: "KWD", Exponent: 3},
	} {
		currencies[c.Code] = c
	}
}

// FormatAmount renders an amount in minor units as a decimal string for
// display, e.g. 12345 USD -> "123.45" and 500 JPY -> "500".
func FormatAmount(minor int64, currency string) string {
	info, ok := currencies[currency]
	if !ok || info.Exponent == 0 {
		return strconv.FormatInt(minor, 10)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	scale := int64(1)
	for i := 0; i < info.Exponent; i++ {
		scale *= 10
	}
	return fmt.Sprintf("%s%d.%0*d", sign, minor/scale, info.Exponent, minor%scale)
}
