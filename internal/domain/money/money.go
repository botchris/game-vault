// Package money holds the ISO 4217 currency rules that prices, custom money fields and the default
// currency share. It is a leaf package so that game, field and settings apply one rule without
// settings importing the catalog domain.
package money

import "strings"

// IsCurrencyCode reports whether s looks like an ISO 4217 code: three letters A–Z.
func IsCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// currencyDigits lists the ISO 4217 currencies whose minor unit is not two digits.
var currencyDigits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0,
	"RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// CurrencyDigits returns how many decimals a currency has (2 unless ISO 4217 says otherwise).
func CurrencyDigits(code string) int {
	if d, ok := currencyDigits[strings.ToUpper(code)]; ok {
		return d
	}

	return 2
}
