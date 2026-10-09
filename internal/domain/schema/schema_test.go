package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate_consent(t *testing.T) {
	fields := Fields{
		{
			Key:      "accept_risk",
			Kind:     FieldConsent,
			Required: true,
		},
		{
			Key:      "token",
			Kind:     FieldSecret,
			Required: true,
		},
	}

	t.Run("GIVEN a required consent that was not ticked", func(t *testing.T) {
		for _, v := range []string{"", "no", "true"} {
			err := fields.Validate(Settings{"accept_risk": v, "token": "x"}, "Store")

			t.Run("THEN the settings are refused with "+v, func(t *testing.T) {
				assert.ErrorContains(t, err, "Store needs you to accept the risk")
			})
		}
	})

	t.Run("GIVEN the consent ticked", func(t *testing.T) {
		err := fields.Validate(Settings{"accept_risk": ConsentGiven, "token": "x"}, "Store")

		t.Run("THEN the other fields are checked as usual", func(t *testing.T) {
			assert.NoError(t, err)
		})
	})
}
