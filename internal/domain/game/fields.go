package game

import (
	"slices"
	"time"
)

// FieldValue is the value of one custom field (see the field package). Exactly the member that
// matches the field's type is set; the field package validates it.
type FieldValue struct {
	// Text is the value of text and long-text fields.
	Text string

	// Bool is the value of yes/no fields.
	Bool *bool

	// Number is the value of number fields, in hundredths when the field has 2 decimals.
	Number *int64

	// Money is the value of money fields.
	Money *Money

	// Date is the value of date fields: "YYYY", "YYYY-MM" or "YYYY-MM-DD".
	Date string

	// Minutes is the value of duration fields.
	Minutes *int64

	// Choice is the value of list fields: the id of one of the field's choices.
	Choice string

	// Choices is the value of multilist fields: ids of the field's choices.
	Choices []string
}

// IsZero reports whether the value is empty. Empty values are never stored.
func (v FieldValue) IsZero() bool {
	return v.Text == "" && v.Bool == nil && v.Number == nil && v.Money == nil && v.Date == "" &&
		v.Minutes == nil && v.Choice == "" && len(v.Choices) == 0
}

func (v FieldValue) clone() FieldValue {
	out := v
	out.Choices = slices.Clone(v.Choices)

	if v.Bool != nil {
		b := *v.Bool
		out.Bool = &b
	}

	if v.Number != nil {
		n := *v.Number
		out.Number = &n
	}

	if v.Money != nil {
		m := *v.Money
		out.Money = &m
	}

	if v.Minutes != nil {
		m := *v.Minutes
		out.Minutes = &m
	}

	return out
}

// FieldValues maps a custom field's id to its value.
type FieldValues map[string]FieldValue

// compact returns a deep copy without empty values, nil when nothing is left.
func (f FieldValues) compact() FieldValues {
	var out FieldValues

	for id, v := range f {
		if v.IsZero() {
			continue
		}

		if out == nil {
			out = FieldValues{}
		}

		out[id] = v.clone()
	}

	return out
}

// Fields returns the game's custom field values (a copy).
func (g *Game) Fields() FieldValues { return g.fields.compact() }

// SetCopyFields replaces a copy's custom field values. The caller validates them against the
// field definitions first.
func (g *Game) SetCopyFields(id ID, values FieldValues, now time.Time) error {
	i := g.indexOf(id)
	if i < 0 {
		return ErrCopyNotFound
	}

	g.copies[i].Fields = values.compact()
	g.copies[i].UpdatedAt = now
	g.updatedAt = now

	return nil
}

// RemoveFieldValues drops a field's values from the game and its copies (the field was deleted).
// It reports whether the game had a value and how many copies had one.
func (g *Game) RemoveFieldValues(fieldID string) (gameHad bool, copies int) {
	if _, ok := g.fields[fieldID]; ok {
		delete(g.fields, fieldID)

		gameHad = true
	}

	for i := range g.copies {
		if _, ok := g.copies[i].Fields[fieldID]; ok {
			delete(g.copies[i].Fields, fieldID)

			copies++
		}
	}

	return gameHad, copies
}

// ReplaceChoice replaces a list value with another in a field's values, or clears it when to is
// empty (the value was removed from the list). A multilist never ends up with the same value
// twice. It reports whether anything changed.
func (g *Game) ReplaceChoice(fieldID, from, to string) bool {
	changed := replaceChoice(g.fields, fieldID, from, to)

	for i := range g.copies {
		if replaceChoice(g.copies[i].Fields, fieldID, from, to) {
			changed = true
		}
	}

	return changed
}

func replaceChoice(values FieldValues, fieldID, from, to string) bool {
	v, ok := values[fieldID]
	if !ok {
		return false
	}

	switch {
	case v.Choice == from:
		v.Choice = to
	case slices.Contains(v.Choices, from):
		next := make([]string, 0, len(v.Choices))
		for _, c := range v.Choices {
			if c == from {
				c = to
			}

			if c != "" && !slices.Contains(next, c) {
				next = append(next, c)
			}
		}

		v.Choices = next
	default:
		return false
	}

	if v.IsZero() {
		delete(values, fieldID)
	} else {
		values[fieldID] = v
	}

	return true
}

// fillFields adds to dst the values of src for fields dst has no value for.
func fillFields(dst, src FieldValues) FieldValues {
	out := dst.compact()

	for id, v := range src.compact() {
		if _, ok := out[id]; ok {
			continue
		}

		if out == nil {
			out = FieldValues{}
		}

		out[id] = v
	}

	return out
}
