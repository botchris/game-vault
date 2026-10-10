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
			out[id] = v.Clone() // the caller keeps its pointers and slices to itself
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
	if !onlyMemberOf(d.Type, v) {
		return v, wrong
	}

	switch d.Type {
	case TypeText, TypeLongText:
		text := strings.TrimSpace(v.Text)

		limit := MaxText
		if d.Type == TypeLongText {
			limit = MaxLongText
		}

		if len([]rune(text)) > limit {
			return v, invalid("%q can have at most %d characters", d.Name, limit)
		}

		return game.FieldValue{Text: text}, nil
	case TypeBool:
		return game.FieldValue{Bool: v.Bool}, nil
	case TypeNumber:
		limit := int64(MaxNumber)
		if d.Decimals == 2 {
			limit *= 100
		}

		if v.Number != nil && (*v.Number > limit || *v.Number < -limit) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Number: v.Number}, nil
	case TypeMoney:
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
		date := strings.TrimSpace(v.Date)
		if date != "" && !validPartialDate(date) {
			return v, invalid("%q must be a year, a year and month, or a full date", d.Name)
		}

		return game.FieldValue{Date: date}, nil
	case TypeDuration:
		if v.Minutes != nil && (*v.Minutes < 0 || *v.Minutes > MaxMinutes) {
			return v, invalid("%q is out of range", d.Name)
		}

		return game.FieldValue{Minutes: v.Minutes}, nil
	case TypeList:
		if _, ok := d.choice(v.Choice); v.Choice != "" && !ok {
			return v, invalid("%q: that value is no longer in the list, reload the page", d.Name)
		}

		return game.FieldValue{Choice: v.Choice}, nil
	case TypeMultiList:
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

// onlyMemberOf reports whether v sets no member but the one of fields of type t. It clears that
// member and checks that nothing is left, so a member added to game.FieldValue later is refused
// for every type without listing it here.
func onlyMemberOf(t Type, v game.FieldValue) bool {
	switch t {
	case TypeText, TypeLongText:
		v.Text = ""
	case TypeBool:
		v.Bool = nil
	case TypeNumber:
		v.Number = nil
	case TypeMoney:
		v.Money = nil
	case TypeDate:
		v.Date = ""
	case TypeDuration:
		v.Minutes = nil
	case TypeList:
		v.Choice = ""
	case TypeMultiList:
		v.Choices = nil
	}

	return v.IsZero()
}

// validPartialDate reports whether s is "YYYY", "YYYY-MM" or "YYYY-MM-DD", an existing date from
// year 1 on (Go parses year 0, which no calendar has).
func validPartialDate(s string) bool {
	for _, layout := range []string{"2006", "2006-01", time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil && len(s) == len(layout) {
			return t.Year() >= 1
		}
	}

	return false
}
