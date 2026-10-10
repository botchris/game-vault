// Package field defines custom fields: the fields users add to games or copies, of nine types,
// and the validation of their values (stored on games as game.FieldValues).
package field

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"gamevault/internal/domain/game"
)

// Type is a field's kind of value.
type Type string

// Values of Type.
const (
	TypeText      Type = "text"
	TypeLongText  Type = "longtext"
	TypeBool      Type = "bool"
	TypeNumber    Type = "number"
	TypeMoney     Type = "money"
	TypeDate      Type = "date"
	TypeDuration  Type = "duration"
	TypeList      Type = "list"
	TypeMultiList Type = "multilist"
)

// Scope says what a field describes: a game, or each of its copies.
type Scope string

// Values of Scope.
const (
	ScopeGame Scope = "game"
	ScopeCopy Scope = "copy"
)

// Limits of fields and values.
const (
	MaxFields     = 50
	MaxChoices    = 200
	MaxNameLength = 60
	MaxText       = 250
	MaxLongText   = 10_000
	MaxNumber     = 1_000_000_000_000
	MaxMinutes    = 100_000 * 60
	maxUnitLength = 10
)

var types = []Type{TypeText, TypeLongText, TypeBool, TypeNumber, TypeMoney, TypeDate, TypeDuration, TypeList, TypeMultiList}

// ErrNotFound means there is no field (or list value) with that id.
var ErrNotFound = errors.New("custom field not found: reload the page")

// ValidationError reports a field definition or value that breaks a rule.
type ValidationError struct{ msg string }

// Error returns the message, which says what to fix.
func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Choice is one value of a list or multilist field.
type Choice struct {
	// ID never changes, so renaming the value renames it everywhere.
	ID string

	// Name is what the user sees.
	Name string
}

// Definition is one custom field.
type Definition struct {
	// ID never changes.
	ID string

	// Name is unique among the fields, ignoring case.
	Name string

	// Type is fixed once the field exists.
	Type Type

	// Scope is fixed once the field exists.
	Scope Scope

	// Kinds limits a copy field to these copy kinds; empty means every kind.
	Kinds []game.Kind

	// Decimals is 0 or 2, for number fields.
	Decimals int

	// Unit is shown after a number ("g", "%"…), for number fields.
	Unit string

	// Currency is the money field's currency (ISO 4217); empty means the default currency.
	Currency string

	// Choices are the values of a list or multilist field, in order.
	Choices []Choice
}

// AppliesTo reports whether a copy field applies to copies of this kind.
func (d Definition) AppliesTo(kind game.Kind) bool {
	return d.Scope == ScopeCopy && (len(d.Kinds) == 0 || slices.Contains(d.Kinds, kind))
}

func (d Definition) clone() Definition {
	d.Kinds = slices.Clone(d.Kinds)
	d.Choices = slices.Clone(d.Choices)

	return d
}

func (d Definition) choice(id string) (Choice, bool) {
	i := slices.IndexFunc(d.Choices, func(c Choice) bool { return c.ID == id })
	if i < 0 {
		return Choice{}, false
	}

	return d.Choices[i], true
}

// Set is the ordered list of custom fields.
type Set struct {
	defs []Definition
}

// NewSet returns a set holding these definitions, in order (from storage).
func NewSet(defs []Definition) *Set {
	s := &Set{}
	for _, d := range defs {
		s.defs = append(s.defs, d.clone())
	}

	return s
}

// Definitions returns the fields in order (copies).
func (s *Set) Definitions() []Definition {
	out := make([]Definition, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d.clone())
	}

	return out
}

// Get returns the field with this id.
func (s *Set) Get(id string) (Definition, bool) {
	i := s.index(id)
	if i < 0 {
		return Definition{}, false
	}

	return s.defs[i].clone(), true
}

func (s *Set) index(id string) int {
	return slices.IndexFunc(s.defs, func(d Definition) bool { return d.ID == id })
}

// Add creates a field, giving it and its choices ids.
func (s *Set) Add(d Definition) (Definition, error) {
	if len(s.defs) >= MaxFields {
		return Definition{}, invalid("there can be at most %d custom fields", MaxFields)
	}

	d.ID = uuid.NewString()
	for i := range d.Choices {
		d.Choices[i].ID = uuid.NewString()
	}

	d, err := s.check(d)
	if err != nil {
		return Definition{}, err
	}

	s.defs = append(s.defs, d)

	return d.clone(), nil
}

// Update changes a field's name, copy kinds, number and money options and list values. The type
// and scope never change, and list values are only removed with RemoveChoice (they are in use).
func (s *Set) Update(d Definition) error {
	i := s.index(d.ID)
	if i < 0 {
		return ErrNotFound
	}

	old := s.defs[i]
	if d.Type != old.Type || d.Scope != old.Scope {
		return invalid("the type of %q and whether it describes a game or a copy cannot change", old.Name)
	}

	for _, c := range old.Choices {
		if !slices.ContainsFunc(d.Choices, func(n Choice) bool { return n.ID == c.ID }) {
			return invalid("%q: remove a value with its own button (it may be in use)", old.Name)
		}
	}

	d.Choices = slices.Clone(d.Choices)
	for j := range d.Choices {
		if d.Choices[j].ID == "" {
			d.Choices[j].ID = uuid.NewString()
		} else if _, ok := old.choice(d.Choices[j].ID); !ok {
			return ErrNotFound
		}
	}

	d, err := s.check(d)
	if err != nil {
		return err
	}

	s.defs[i] = d

	return nil
}

// Move puts a field at index (clamped to the list).
func (s *Set) Move(id string, index int) error {
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}

	d := s.defs[i]
	s.defs = slices.Delete(s.defs, i, i+1)
	index = max(0, min(index, len(s.defs)))
	s.defs = slices.Insert(s.defs, index, d)

	return nil
}

// Remove deletes a field. Its values must be removed from games in the same transaction.
func (s *Set) Remove(id string) error {
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}

	s.defs = slices.Delete(s.defs, i, i+1)

	return nil
}

// AddChoice adds a value to a list or multilist field, or returns the existing one with that name
// (ignoring case).
func (s *Set) AddChoice(fieldID, name string) (Choice, error) {
	i := s.index(fieldID)
	if i < 0 {
		return Choice{}, ErrNotFound
	}

	d := s.defs[i]
	name = strings.TrimSpace(name)

	if j := slices.IndexFunc(d.Choices, func(c Choice) bool { return strings.EqualFold(c.Name, name) }); j >= 0 {
		return d.Choices[j], nil
	}

	c := Choice{
		ID:   uuid.NewString(),
		Name: name,
	}
	d.Choices = append(slices.Clone(d.Choices), c)

	d, err := s.check(d)
	if err != nil {
		return Choice{}, err
	}

	s.defs[i] = d

	return c, nil
}

// RemoveChoice removes a value from a list or multilist field. mergeInto, when set, is the value
// that replaces it in games (it must be another value of the field); games are updated by the
// caller in the same transaction.
func (s *Set) RemoveChoice(fieldID, choiceID, mergeInto string) error {
	i := s.index(fieldID)
	if i < 0 {
		return ErrNotFound
	}

	d := s.defs[i]
	if _, ok := d.choice(choiceID); !ok {
		return ErrNotFound
	}

	if mergeInto != "" {
		if _, ok := d.choice(mergeInto); !ok || mergeInto == choiceID {
			return invalid("%q: choose another value of the list to merge into", d.Name)
		}
	}

	d.Choices = slices.DeleteFunc(slices.Clone(d.Choices), func(c Choice) bool { return c.ID == choiceID })
	s.defs[i] = d

	return nil
}

// check normalizes a definition and enforces the rules, against the other fields of the set.
func (s *Set) check(d Definition) (Definition, error) {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > MaxNameLength {
		return d, invalid("a field needs a name of 1 to %d characters", MaxNameLength)
	}

	for _, o := range s.defs {
		if o.ID != d.ID && strings.EqualFold(o.Name, d.Name) {
			return d, invalid("there is already a field named %q", o.Name)
		}
	}

	if !slices.Contains(types, d.Type) {
		return d, invalid("%q: %q is not a field type", d.Name, d.Type)
	}

	if d.Scope != ScopeGame && d.Scope != ScopeCopy {
		return d, invalid("%q must describe a game or a copy", d.Name)
	}

	if d.Scope == ScopeGame && len(d.Kinds) > 0 {
		return d, invalid("%q: only copy fields can be limited to some copy kinds", d.Name)
	}

	for _, k := range d.Kinds {
		if !k.Valid() {
			return d, invalid("%q: %q is not a copy kind", d.Name, k)
		}
	}

	d.Unit = strings.TrimSpace(d.Unit)
	if d.Type != TypeNumber && (d.Decimals != 0 || d.Unit != "") {
		return d, invalid("%q: decimals and units are for number fields", d.Name)
	}

	if d.Decimals != 0 && d.Decimals != 2 {
		return d, invalid("%q: a number has 0 or 2 decimals", d.Name)
	}

	if len([]rune(d.Unit)) > maxUnitLength {
		return d, invalid("%q: the unit can have at most %d characters", d.Name, maxUnitLength)
	}

	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	if d.Type != TypeMoney && d.Currency != "" {
		return d, invalid("%q: a currency is for money fields", d.Name)
	}

	if d.Currency != "" && !isCurrencyCode(d.Currency) {
		return d, invalid("%q: the currency must be a three-letter code (EUR, USD…)", d.Name)
	}

	isList := d.Type == TypeList || d.Type == TypeMultiList
	if !isList && len(d.Choices) > 0 {
		return d, invalid("%q: only list fields have values to choose from", d.Name)
	}

	if len(d.Choices) > MaxChoices {
		return d, invalid("%q can have at most %d values", d.Name, MaxChoices)
	}

	for i, c := range d.Choices {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || len([]rune(c.Name)) > MaxNameLength {
			return d, invalid("%q: each value needs a name of 1 to %d characters", d.Name, MaxNameLength)
		}

		if slices.ContainsFunc(d.Choices[:i], func(o Choice) bool { return strings.EqualFold(strings.TrimSpace(o.Name), c.Name) }) {
			return d, invalid("%q has the value %q twice", d.Name, c.Name)
		}

		d.Choices[i] = c
	}

	return d.clone(), nil
}

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}

// Repository stores the custom field definitions.
type Repository interface {
	// Fields returns the definitions, an empty set when none were saved.
	Fields(ctx context.Context) (*Set, error)

	// SaveFields replaces the stored definitions.
	SaveFields(ctx context.Context, s *Set) error
}
