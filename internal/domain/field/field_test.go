package field

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
)

func i64(n int64) *int64 { return &n }

func TestDefinitions(t *testing.T) {
	t.Run("GIVEN a new set", func(t *testing.T) {
		s := NewSet(nil)

		t.Run("THEN fields get ids, names are unique and choices get ids", func(t *testing.T) {
			d, err := s.Add(Definition{
				Name:    "Given to",
				Type:    TypeList,
				Scope:   ScopeCopy,
				Choices: []Choice{{Name: "Ana"}},
			})
			require.NoError(t, err)
			assert.NotEmpty(t, d.ID)
			assert.NotEmpty(t, d.Choices[0].ID)

			_, err = s.Add(Definition{
				Name:  "given TO",
				Type:  TypeText,
				Scope: ScopeGame,
			})
			assert.Error(t, err, "names are unique, ignoring case")
		})

		t.Run("THEN broken definitions are refused", func(t *testing.T) {
			for name, d := range map[string]Definition{
				"no name": {
					Type:  TypeText,
					Scope: ScopeGame,
				},
				"unknown type": {
					Name:  "X",
					Type:  "color",
					Scope: ScopeGame,
				},
				"unknown scope": {
					Name:  "X",
					Type:  TypeText,
					Scope: "shelf",
				},
				"3 decimals": {
					Name:     "X",
					Type:     TypeNumber,
					Scope:    ScopeGame,
					Decimals: 3,
				},
				"kinds on a game": {
					Name:  "X",
					Type:  TypeText,
					Scope: ScopeGame,
					Kinds: []game.Kind{game.KindKey},
				},
				"bad currency": {
					Name:     "X",
					Type:     TypeMoney,
					Scope:    ScopeGame,
					Currency: "euro",
				},
				"repeated choice": {
					Name:    "X",
					Type:    TypeList,
					Scope:   ScopeGame,
					Choices: []Choice{{Name: "A"}, {Name: "a"}},
				},
				"choices on a text": {
					Name:    "X",
					Type:    TypeText,
					Scope:   ScopeGame,
					Choices: []Choice{{Name: "A"}},
				},
			} {
				_, err := NewSet(nil).Add(d)

				var v *ValidationError
				assert.True(t, errors.As(err, &v), name)
			}
		})
	})

	t.Run("GIVEN a list field WHEN it is updated", func(t *testing.T) {
		s := NewSet(nil)
		d, _ := s.Add(Definition{
			Name:    "Shelf",
			Type:    TypeList,
			Scope:   ScopeGame,
			Choices: []Choice{{Name: "A"}},
		})

		t.Run("THEN the type and scope cannot change", func(t *testing.T) {
			changed := d
			changed.Type = TypeText
			assert.Error(t, s.Update(changed))
		})

		t.Run("THEN choices are renamed in place and new ones get ids, but none disappears", func(t *testing.T) {
			changed := d
			changed.Choices = []Choice{{
				ID:   d.Choices[0].ID,
				Name: "Top",
			}, {Name: "Bottom"}}
			require.NoError(t, s.Update(changed))
			got, _ := s.Get(d.ID)
			assert.Equal(t, "Top", got.Choices[0].Name)
			assert.NotEmpty(t, got.Choices[1].ID)

			changed.Choices = got.Choices[1:]
			assert.Error(t, s.Update(changed), "removing a value goes through RemoveChoice")
		})

		t.Run("THEN AddChoice reuses an existing name", func(t *testing.T) {
			got, _ := s.Get(d.ID)
			c, err := s.AddChoice(d.ID, "top")
			require.NoError(t, err)
			assert.Equal(t, got.Choices[0].ID, c.ID)
		})
	})

	t.Run("GIVEN 50 fields THEN a 51st is refused", func(t *testing.T) {
		s := NewSet(nil)
		for i := range MaxFields {
			_, err := s.Add(Definition{
				Name:  strings.Repeat("x", i+1),
				Type:  TypeText,
				Scope: ScopeGame,
			})
			require.NoError(t, err)
		}

		_, err := s.Add(Definition{
			Name:  "one more",
			Type:  TypeText,
			Scope: ScopeGame,
		})
		assert.Error(t, err)
	})
}

func TestValidate(t *testing.T) {
	s := NewSet(nil)
	text, _ := s.Add(Definition{
		Name:  "By",
		Type:  TypeText,
		Scope: ScopeGame,
	})
	weight, _ := s.Add(Definition{
		Name:     "Weight",
		Type:     TypeNumber,
		Scope:    ScopeCopy,
		Decimals: 2,
		Unit:     "g",
	})
	sealed, _ := s.Add(Definition{
		Name:  "Sealed",
		Type:  TypeBool,
		Scope: ScopeCopy,
		Kinds: []game.Kind{game.KindPhysical},
	})
	tags, _ := s.Add(Definition{
		Name:    "Awards",
		Type:    TypeMultiList,
		Scope:   ScopeGame,
		Choices: []Choice{{Name: "A"}, {Name: "B"}},
	})
	day, _ := s.Add(Definition{
		Name:  "Gifted",
		Type:  TypeDate,
		Scope: ScopeGame,
	})
	cost, _ := s.Add(Definition{
		Name:     "Shipping",
		Type:     TypeMoney,
		Scope:    ScopeCopy,
		Currency: "EUR",
	})
	yes := true

	t.Run("GIVEN valid values THEN they are normalized", func(t *testing.T) {
		got, err := s.Validate(ScopeGame, "", game.FieldValues{
			text.ID: {Text: "  A friend  "},
			tags.ID: {Choices: []string{tags.Choices[1].ID, tags.Choices[0].ID, tags.Choices[1].ID}},
			day.ID:  {Date: "2024-02"},
		})
		require.NoError(t, err)
		assert.Equal(t, "A friend", got[text.ID].Text)
		assert.Equal(t, []string{tags.Choices[0].ID, tags.Choices[1].ID}, got[tags.ID].Choices, "field order, no repeats")
		assert.Equal(t, "2024-02", got[day.ID].Date)

		got, err = s.Validate(ScopeCopy, game.KindPhysical, game.FieldValues{
			weight.ID: {Number: i64(45050)},
			sealed.ID: {Bool: &yes},
			cost.ID:   {Money: &game.Money{Amount: 499}},
		})
		require.NoError(t, err)
		assert.Equal(t, "EUR", got[cost.ID].Money.Currency, "the field's currency fills in")
	})

	t.Run("GIVEN invalid values THEN each is refused, naming the field", func(t *testing.T) {
		for name, tc := range map[string]struct {
			scope  Scope
			kind   game.Kind
			values game.FieldValues
		}{
			"unknown field":        {ScopeGame, "", game.FieldValues{"gone": {Text: "x"}}},
			"wrong type":           {ScopeGame, "", game.FieldValues{text.ID: {Bool: &yes}}},
			"copy field on a game": {ScopeGame, "", game.FieldValues{weight.ID: {Number: i64(1)}}},
			"kind not allowed":     {ScopeCopy, game.KindKey, game.FieldValues{sealed.ID: {Bool: &yes}}},
			"unknown choice":       {ScopeGame, "", game.FieldValues{tags.ID: {Choices: []string{"nope"}}}},
			"bad date":             {ScopeGame, "", game.FieldValues{day.ID: {Date: "2024-13"}}},
			"too long":             {ScopeGame, "", game.FieldValues{text.ID: {Text: strings.Repeat("x", 251)}}},
			"too big":              {ScopeCopy, game.KindPhysical, game.FieldValues{weight.ID: {Number: i64(MaxNumber*100 + 1)}}},
			"other currency": {ScopeCopy, game.KindPhysical, game.FieldValues{cost.ID: {Money: &game.Money{
				Amount:   1,
				Currency: "USD",
			}}}},
		} {
			_, err := s.Validate(tc.scope, tc.kind, tc.values)

			var v *ValidationError
			assert.True(t, errors.As(err, &v), name)
		}
	})

	t.Run("GIVEN a removed choice with a merge target THEN RemoveChoice keeps the field consistent", func(t *testing.T) {
		require.NoError(t, s.RemoveChoice(tags.ID, tags.Choices[0].ID, tags.Choices[1].ID))
		got, _ := s.Get(tags.ID)
		assert.Len(t, got.Choices, 1)
		assert.Error(t, s.RemoveChoice(tags.ID, got.Choices[0].ID, "nope"), "the merge target must exist")
	})
}
