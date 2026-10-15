package home

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/provider"
)

func TestCountryFor(t *testing.T) {
	for currency, want := range map[string]string{
		"EUR": "ES", "USD": "US", "GBP": "GB", "JPY": "JP", "BRL": "BR", "eur": "ES", "": "", "XYZ": "",
	} {
		assert.Equal(t, want, countryFor(currency), currency)
	}
}

func TestKindOf(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want ErrorKind
	}{
		{provider.ErrQuotaExhausted, ErrorQuota},
		{provider.BadSettings("check the key"), ErrorSettings},
		{provider.ErrUnavailable, ErrorUnavailable},
		{provider.ErrRateLimited, ErrorRateLimited},
		{fmt.Errorf("unexpected answer"), ErrorFailed},
	} {
		assert.Equal(t, tc.want, kindOf(fmt.Errorf("deals: %w", tc.err)), tc.err.Error())
	}
}
