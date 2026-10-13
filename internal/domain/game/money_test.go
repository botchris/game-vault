package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoney(t *testing.T) {
	cases := []struct {
		name string
		in   Money
		want Money
		ok   bool
	}{
		{"a price is kept, currency upper-cased", Money{
			Amount:   2995,
			Currency: " eur ",
		}, Money{
			Amount:   2995,
			Currency: "EUR",
		}, true},
		{"no amount means no price", Money{
			Amount:   0,
			Currency: "EUR",
		}, Money{}, true},
		{"an amount needs a currency", Money{Amount: 100}, Money{}, false},
		{"a currency is three letters", Money{
			Amount:   100,
			Currency: "EURO",
		}, Money{}, false},
		{"no negative prices", Money{
			Amount:   -1,
			Currency: "EUR",
		}, Money{}, false},
	}
	for _, c := range cases {
		got, err := c.in.normalize()
		if !c.ok {
			assert.Error(t, err, c.name)
			continue
		}

		require.NoError(t, err, c.name)
		assert.Equal(t, c.want, got, c.name)
	}
}
