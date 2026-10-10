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

func TestSet_Add_doesNotChangeTheCallersChoices(t *testing.T) {
	t.Run("GIVEN a definition with untrimmed choices without ids", func(t *testing.T) {
		choices := []Choice{{Name: " Ana "}}
		d := Definition{
			Name:    "Given to",
			Type:    TypeList,
			Scope:   ScopeGame,
			Choices: choices,
		}

		t.Run("WHEN it is added", func(t *testing.T) {
			got, err := NewSet(nil).Add(d)
			require.NoError(t, err)

			t.Run("THEN the field has ids and trimmed names but the caller's slice is untouched", func(t *testing.T) {
				assert.NotEmpty(t, got.Choices[0].ID)
				assert.Equal(t, "Ana", got.Choices[0].Name)
				assert.Equal(t, []Choice{{Name: " Ana "}}, choices)
			})
		})
	})
}

func TestSet_Update_choiceIDs(t *testing.T) {
	t.Run("GIVEN a list field with one value", func(t *testing.T) {
		s := NewSet(nil)
		d, err := s.Add(Definition{
			Name:    "Shelf",
			Type:    TypeList,
			Scope:   ScopeGame,
			Choices: []Choice{{Name: "Top"}},
		})
		require.NoError(t, err)

		t.Run("WHEN it is updated with a value id it never had", func(t *testing.T) {
			changed := d
			changed.Choices = []Choice{d.Choices[0], {
				ID:   "nope",
				Name: "Bottom",
			}}
			err := s.Update(changed)

			t.Run("THEN it is refused as not found", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrNotFound)
			})
		})

		t.Run("WHEN it is updated with the same value id twice", func(t *testing.T) {
			changed := d
			changed.Choices = []Choice{d.Choices[0], {
				ID:   d.Choices[0].ID,
				Name: "Bottom",
			}}
			err := s.Update(changed)

			t.Run("THEN it is refused and the field is unchanged", func(t *testing.T) {
				var v *ValidationError
				require.ErrorAs(t, err, &v)

				got, _ := s.Get(d.ID)
				assert.Equal(t, d.Choices, got.Choices)
			})
		})
	})
}

func TestSet_names_boundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		field  string
		choice string
		ok     bool
	}{
		{"field name of 60 characters", strings.Repeat("é", MaxNameLength), "A", true},
		{"field name of 61 characters", strings.Repeat("é", MaxNameLength+1), "A", false},
		{"value name of 60 characters", "X", strings.Repeat("é", MaxNameLength), true},
		{"value name of 61 characters", "X", strings.Repeat("é", MaxNameLength+1), false},
	} {
		_, err := NewSet(nil).Add(Definition{
			Name:    tc.field,
			Type:    TypeList,
			Scope:   ScopeGame,
			Choices: []Choice{{Name: tc.choice}},
		})
		if tc.ok {
			assert.NoError(t, err, tc.name)
		} else {
			var v *ValidationError
			assert.ErrorAs(t, err, &v, tc.name)
		}
	}
}

// oneOfEach returns a set with a game field of every type (the lists with one value) and their
// definitions by type.
func oneOfEach(t *testing.T) (*Set, map[Type]Definition) {
	t.Helper()

	s := NewSet(nil)
	defs := map[Type]Definition{}

	for _, typ := range types {
		d := Definition{
			Name:  string(typ),
			Type:  typ,
			Scope: ScopeGame,
		}
		if typ == TypeList || typ == TypeMultiList {
			d.Choices = []Choice{{Name: "A"}}
		}

		added, err := s.Add(d)
		require.NoError(t, err)

		defs[typ] = added
	}

	return s, defs
}

func TestValidate_wrongMember(t *testing.T) {
	s, defs := oneOfEach(t)
	yes := true

	// right holds, by type, a valid value of that type.
	right := map[Type]game.FieldValue{
		TypeText:     {Text: "x"},
		TypeLongText: {Text: "x"},
		TypeBool:     {Bool: &yes},
		TypeNumber:   {Number: i64(1)},
		TypeMoney:    {Money: &game.Money{Amount: 1}},
		TypeDate:     {Date: "2024"},
		TypeDuration: {Minutes: i64(1)},
		TypeList:     {Choice: defs[TypeList].Choices[0].ID},
		TypeMultiList: {Choices: []string{
			defs[TypeMultiList].Choices[0].ID,
		}},
	}

	// alone holds, by type, a value that only sets a member of another type.
	alone := map[Type]game.FieldValue{
		TypeText:      {Bool: &yes},
		TypeLongText:  {Number: i64(1)},
		TypeBool:      {Text: "x"},
		TypeNumber:    {Minutes: i64(1)},
		TypeMoney:     {Number: i64(1)},
		TypeDate:      {Choice: "x"},
		TypeDuration:  {Number: i64(1)},
		TypeList:      {Choices: []string{"x"}},
		TypeMultiList: {Choice: "x"},
	}

	t.Run("GIVEN a field of every type", func(t *testing.T) {
		for _, typ := range types {
			id := defs[typ].ID

			t.Run("WHEN a "+string(typ)+" gets its own value THEN it is accepted", func(t *testing.T) {
				_, err := s.Validate(ScopeGame, "", game.FieldValues{id: right[typ]})
				assert.NoError(t, err)
			})

			t.Run("WHEN a "+string(typ)+" gets only a member of another type THEN it is refused", func(t *testing.T) {
				_, err := s.Validate(ScopeGame, "", game.FieldValues{id: alone[typ]})

				var v *ValidationError
				require.ErrorAs(t, err, &v)
				assert.Contains(t, err.Error(), "got a value of another type")
			})

			t.Run("WHEN a "+string(typ)+" gets its value plus a member of another type THEN it is refused", func(t *testing.T) {
				for _, other := range types {
					if right[other].Text != "" && right[typ].Text != "" {
						continue // text and long text share their member
					}

					if other == typ {
						continue
					}

					v := right[typ]
					extra := right[other]

					switch other {
					case TypeText, TypeLongText:
						v.Text = extra.Text
					case TypeBool:
						v.Bool = extra.Bool
					case TypeNumber:
						v.Number = extra.Number
					case TypeMoney:
						v.Money = extra.Money
					case TypeDate:
						v.Date = extra.Date
					case TypeDuration:
						v.Minutes = extra.Minutes
					case TypeList:
						v.Choice = extra.Choice
					case TypeMultiList:
						v.Choices = extra.Choices
					}

					_, err := s.Validate(ScopeGame, "", game.FieldValues{id: v})

					var verr *ValidationError
					assert.ErrorAs(t, err, &verr, "%s with a %s member", typ, other)
				}
			})
		}
	})
}

func TestValidate_boundaries(t *testing.T) {
	s := NewSet(nil)
	add := func(d Definition) string {
		d.Scope = ScopeGame
		added, err := s.Add(d)
		require.NoError(t, err)

		return added.ID
	}

	text := add(Definition{
		Name: "Text",
		Type: TypeText,
	})
	long := add(Definition{
		Name: "Long",
		Type: TypeLongText,
	})
	number := add(Definition{
		Name: "Number",
		Type: TypeNumber,
	})
	cents := add(Definition{
		Name:     "Cents",
		Type:     TypeNumber,
		Decimals: 2,
	})
	money := add(Definition{
		Name: "Money",
		Type: TypeMoney,
	})
	duration := add(Definition{
		Name: "Duration",
		Type: TypeDuration,
	})

	for _, tc := range []struct {
		name  string
		value game.FieldValues
		ok    bool
	}{
		{"text of 250 characters", game.FieldValues{text: {Text: strings.Repeat("é", MaxText)}}, true},
		{"text of 251 characters", game.FieldValues{text: {Text: strings.Repeat("é", MaxText+1)}}, false},
		{"long text of 10000 characters", game.FieldValues{long: {Text: strings.Repeat("é", MaxLongText)}}, true},
		{"long text of 10001 characters", game.FieldValues{long: {Text: strings.Repeat("é", MaxLongText+1)}}, false},
		{"number 1e12", game.FieldValues{number: {Number: i64(MaxNumber)}}, true},
		{"number -1e12", game.FieldValues{number: {Number: i64(-MaxNumber)}}, true},
		{"number 1e12+1", game.FieldValues{number: {Number: i64(MaxNumber + 1)}}, false},
		{"number -1e12-1", game.FieldValues{number: {Number: i64(-MaxNumber - 1)}}, false},
		{"number 1e12.00", game.FieldValues{cents: {Number: i64(MaxNumber * 100)}}, true},
		{"number -1e12.00", game.FieldValues{cents: {Number: i64(-MaxNumber * 100)}}, true},
		{"number 1e12.01", game.FieldValues{cents: {Number: i64(MaxNumber*100 + 1)}}, false},
		{"number -1e12.01", game.FieldValues{cents: {Number: i64(-MaxNumber*100 - 1)}}, false},
		{"money 0", game.FieldValues{money: {Money: &game.Money{Amount: 0}}}, true},
		{"money 1e12.00", game.FieldValues{money: {Money: &game.Money{Amount: MaxNumber * 100}}}, true},
		{"money 1e12.01", game.FieldValues{money: {Money: &game.Money{Amount: MaxNumber*100 + 1}}}, false},
		{"money -0.01", game.FieldValues{money: {Money: &game.Money{Amount: -1}}}, false},
		{"money in a bad currency", game.FieldValues{money: {Money: &game.Money{
			Amount:   1,
			Currency: "EURO",
		}}}, false},
		{"duration 0", game.FieldValues{duration: {Minutes: i64(0)}}, true},
		{"duration 6,000,000 minutes", game.FieldValues{duration: {Minutes: i64(6_000_000)}}, true},
		{"duration 6,000,001 minutes", game.FieldValues{duration: {Minutes: i64(6_000_001)}}, false},
		{"duration -1 minute", game.FieldValues{duration: {Minutes: i64(-1)}}, false},
	} {
		_, err := s.Validate(ScopeGame, "", tc.value)
		if tc.ok {
			assert.NoError(t, err, tc.name)
		} else {
			var v *ValidationError
			assert.ErrorAs(t, err, &v, tc.name)
		}
	}
}

func TestValidate_dates(t *testing.T) {
	s := NewSet(nil)
	day, err := s.Add(Definition{
		Name:  "Gifted",
		Type:  TypeDate,
		Scope: ScopeGame,
	})
	require.NoError(t, err)

	for date, want := range map[string]string{
		"2024":         "2024",
		"2024-02":      "2024-02",
		"2024-02-29":   "2024-02-29",
		" 2024-03-05 ": "2024-03-05",
		"0001-01-01":   "0001-01-01",
		"9999-12-31":   "9999-12-31",
	} {
		got, err := s.Validate(ScopeGame, "", game.FieldValues{day.ID: {Date: date}})
		if assert.NoError(t, err, date) {
			assert.Equal(t, want, got[day.ID].Date, date)
		}
	}

	for _, date := range []string{
		"0000", "0000-01", "0000-01-01", "2023-02-29", "2024-13", "2024-00", "2024-01-32",
		"2024-1-5", "24", "20240105", "2024/01/05", "2024-01-05T10:00", "+2024", "next year",
	} {
		_, err := s.Validate(ScopeGame, "", game.FieldValues{day.ID: {Date: date}})

		var v *ValidationError
		assert.ErrorAs(t, err, &v, date)
	}
}

func TestValidate_returnsItsOwnValues(t *testing.T) {
	s, defs := oneOfEach(t)

	t.Run("GIVEN values with pointers and slices", func(t *testing.T) {
		yes := true
		number := int64(5)
		minutes := int64(90)
		money := game.Money{Amount: 100}
		in := game.FieldValues{
			defs[TypeBool].ID:     {Bool: &yes},
			defs[TypeNumber].ID:   {Number: &number},
			defs[TypeMoney].ID:    {Money: &money},
			defs[TypeDuration].ID: {Minutes: &minutes},
		}

		t.Run("WHEN they are validated and the input changes afterwards", func(t *testing.T) {
			got, err := s.Validate(ScopeGame, "", in)
			require.NoError(t, err)

			yes, number, minutes, money.Amount = false, 6, 91, 200

			t.Run("THEN the validated values do not change", func(t *testing.T) {
				assert.True(t, *got[defs[TypeBool].ID].Bool)
				assert.Equal(t, int64(5), *got[defs[TypeNumber].ID].Number)
				assert.Equal(t, int64(90), *got[defs[TypeDuration].ID].Minutes)
				assert.Equal(t, int64(100), got[defs[TypeMoney].ID].Money.Amount)
			})
		})
	})
}
