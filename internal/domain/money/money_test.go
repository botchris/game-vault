package money

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCurrencyCode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"EUR", true},
		{"JPY", true},
		{"eur", false},
		{"EURO", false},
		{"EU", false},
		{"E1R", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, IsCurrencyCode(c.in), "%q", c.in)
	}
}

func TestCurrencyDigits(t *testing.T) {
	assert.Equal(t, 2, CurrencyDigits("EUR"))
	assert.Equal(t, 0, CurrencyDigits("JPY"))
	assert.Equal(t, 3, CurrencyDigits("BHD"))
	assert.Equal(t, 3, CurrencyDigits("bhd"), "codes are compared in upper case")
	assert.Equal(t, 2, CurrencyDigits("XYZ"), "unknown codes default to 2")
}
