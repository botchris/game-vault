package game

import (
	"strings"

	"gamevault/internal/domain/money"
)

// Money is an amount of an ISO 4217 currency, in that currency's minor unit (cents for EUR and
// USD, yen for JPY, fils for BHD): see money.CurrencyDigits. The zero value means no price.
type Money struct {
	Amount   int64
	Currency string
}

// IsZero reports whether there is no price.
func (m Money) IsZero() bool { return m.Amount == 0 }

func (m Money) normalize() (Money, error) {
	m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))

	switch {
	case m.Amount < 0:
		return Money{}, invalid("a price cannot be negative")
	case m.Amount == 0:
		return Money{}, nil
	case !money.IsCurrencyCode(m.Currency):
		return Money{}, invalid("a price needs its currency as a three-letter code (EUR, USD…)")
	}

	return m, nil
}
