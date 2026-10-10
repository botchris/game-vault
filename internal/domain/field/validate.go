package field

import (
	"slices"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

// Validate checks values for a game (scope game, kind empty) or for a copy of the given kind, and
// returns them normalized: text trimmed, multilist choices in the field's order without repeats,
// the field's currency filled in, empty values dropped. Unknown fields are refused (the page may
// be older than a deleted field).
func (s *Set) Validate(scope Scope, kind game.Kind, values game.FieldValues) (game.FieldValues, error) {
	out := game.FieldValues{}

	for id, v := range values {
		d, ok := s.Get(id)
		if !ok {
			return nil, invalid("a custom field no longer exists: reload the page and try again")
		}

		if d.Scope != scope {
			return nil, invalid("%q describes a %s, not a %s", d.Name, d.Scope, scope)
		}

		if scope == ScopeCopy && !d.AppliesTo(kind) {
			return nil, invalid("%q does not apply to %s copies", d.Name, kind)
		}

		v, err := d.normalize(v)
		if err != nil {
			return nil, err
		}

		if !v.IsZero() {
			out[id] = v
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	return out, nil
}

// normalize checks that only the member of the field's type is set and that it is within limits.
func (d Definition) normalize(v game.FieldValue) (game.FieldValue, error) {
	wrong := invalid("%q got a value of another type: reload the page and try again", d.Name)

	switch d.Type {
	case TypeText, TypeLongText:
		text := strings.TrimSpace(v.Text)
		if !(game.FieldValue{
			Bool:    v.Bool,
			Number:  v.Number,
			Money:   v.Money,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		limit := MaxText
		if d.Type == TypeLongText {
			limit = MaxLongText
		}

		if len([]rune(text)) > limit {
			return v, invalid("%q can have at most %d characters", d.Name, limit)
		}

		return game.FieldValue{Text: text}, nil
	case TypeBool:
		if !(game.FieldValue{
			Text:    v.Text,
			Number:  v.Number,
			Money:   v.Money,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		return game.FieldValue{Bool: v.Bool}, nil
	case TypeNumber:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Money:   v.Money,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		limit := int64(MaxNumber)
		if d.Decimals == 2 {
			limit *= 100
		}

		if v.Number != nil && (*v.Number > limit || *v.Number < -limit) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Number: v.Number}, nil
	case TypeMoney:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		if v.Money == nil {
			return game.FieldValue{}, nil
		}

		m := *v.Money
		m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))

		if m.Currency == "" {
			m.Currency = d.Currency
		}

		if d.Currency != "" && m.Currency != d.Currency {
			return v, invalid("%q is in %s", d.Name, d.Currency)
		}

		if m.Amount < 0 || m.Amount > MaxNumber*100 || (m.Currency != "" && !isCurrencyCode(m.Currency)) {
			return v, invalid("%q is not a valid amount", d.Name)
		}

		return game.FieldValue{Money: &m}, nil
	case TypeDate:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Money:   v.Money,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		date := strings.TrimSpace(v.Date)
		if date != "" && !validPartialDate(date) {
			return v, invalid("%q must be a year, a year and month, or a full date", d.Name)
		}

		return game.FieldValue{Date: date}, nil
	case TypeDuration:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Money:   v.Money,
			Date:    v.Date,
			Choice:  v.Choice,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		if v.Minutes != nil && (*v.Minutes < 0 || *v.Minutes > MaxMinutes) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Minutes: v.Minutes}, nil
	case TypeList:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Money:   v.Money,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choices: v.Choices,
		}).IsZero() {
			return v, wrong
		}

		if _, ok := d.choice(v.Choice); v.Choice != "" && !ok {
			return v, invalid("%q: that value is no longer in the list, reload the page", d.Name)
		}

		return game.FieldValue{Choice: v.Choice}, nil
	case TypeMultiList:
		if !(game.FieldValue{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Money:   v.Money,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
		}).IsZero() {
			return v, wrong
		}

		var ids []string

		for _, c := range d.Choices {
			if slices.Contains(v.Choices, c.ID) {
				ids = append(ids, c.ID)
			}
		}

		for _, id := range v.Choices {
			if !slices.Contains(ids, id) {
				return v, invalid("%q: a value is no longer in the list, reload the page", d.Name)
			}
		}

		return game.FieldValue{Choices: ids}, nil
	default:
		return v, wrong
	}
}

func validPartialDate(s string) bool {
	for _, layout := range []string{"2006", "2006-01", time.DateOnly} {
		if _, err := time.Parse(layout, s); err == nil && len(s) == len(layout) {
			return true
		}
	}

	return false
}
