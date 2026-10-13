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

func TestFields_MergeState(t *testing.T) {
	fields := Fields{
		{
			Key:  "code",
			Kind: FieldSecret,
		},
		{
			Key:  "session",
			Kind: FieldState,
		},
		{
			Key:  "hits",
			Kind: FieldState,
		},
	}

	t.Run("GIVEN stored settings with a session", func(t *testing.T) {
		stored := Settings{"code": "abc", "session": "s1", "hits": "es:1"}

		t.Run("WHEN the integration reports the same state", func(t *testing.T) {
			merged, changed := fields.MergeState(stored, Settings{"session": "s1", "hits": "es:1", "code": "other"})

			t.Run("THEN nothing changed and the stored settings come back", func(t *testing.T) {
				assert.False(t, changed)
				assert.Equal(t, stored, merged)
			})
		})

		t.Run("WHEN it reports a rotated session, drops a state field and changes a user setting", func(t *testing.T) {
			merged, changed := fields.MergeState(stored, Settings{"session": "s2", "code": "ignored"})

			t.Run("THEN the new state replaces the old and the user's settings are kept", func(t *testing.T) {
				assert.True(t, changed)
				assert.Equal(t, Settings{"code": "abc", "session": "s2"}, merged)
			})

			t.Run("AND the stored settings are not modified", func(t *testing.T) {
				assert.Equal(t, Settings{"code": "abc", "session": "s1", "hits": "es:1"}, stored)
			})
		})
	})
}
