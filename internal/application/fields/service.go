// Package fields implements the use cases to define custom fields and to apply the changes that
// touch every game holding a value (deleting a field, removing or merging a list value).
package fields

import (
	"context"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

// Service manages custom field definitions.
type Service struct {
	defs  field.Repository
	games game.Repository
	tx    port.TxManager
}

// NewService builds the service.
func NewService(defs field.Repository, games game.Repository, tx port.TxManager) *Service {
	return &Service{
		defs:  defs,
		games: games,
		tx:    tx,
	}
}

// List returns the fields in order.
func (s *Service) List(ctx context.Context) ([]field.Definition, error) {
	set, err := s.defs.Fields(ctx)
	if err != nil {
		return nil, err
	}

	return set.Definitions(), nil
}

// Create adds a field at the end of the list.
func (s *Service) Create(ctx context.Context, d field.Definition) (field.Definition, error) {
	var out field.Definition

	err := s.edit(ctx, func(set *field.Set) error {
		var err error

		out, err = set.Add(d)

		return err
	})

	return out, err
}

// Update changes a field (see field.Set.Update).
func (s *Service) Update(ctx context.Context, d field.Definition) (field.Definition, error) {
	var out field.Definition

	err := s.edit(ctx, func(set *field.Set) error {
		if err := set.Update(d); err != nil {
			return err
		}

		out, _ = set.Get(d.ID)

		return nil
	})

	return out, err
}

// Move puts a field at index.
func (s *Service) Move(ctx context.Context, id string, index int) error {
	return s.edit(ctx, func(set *field.Set) error { return set.Move(id, index) })
}

// AddChoice adds a value to a list field, or returns the existing one with that name.
func (s *Service) AddChoice(ctx context.Context, fieldID, name string) (field.Choice, error) {
	var out field.Choice

	err := s.edit(ctx, func(set *field.Set) error {
		var err error

		out, err = set.AddChoice(fieldID, name)

		return err
	})

	return out, err
}

// Usage counts the games and copies that hold a value for the field, to warn before deleting it.
func (s *Service) Usage(ctx context.Context, id string) (games, copies int, err error) {
	list, err := s.games.List(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, g := range list {
		if _, ok := g.Fields()[id]; ok {
			games++
		}

		for _, c := range g.Copies() {
			if _, ok := c.Fields[id]; ok {
				copies++
			}
		}
	}

	return games, copies, nil
}

// Delete removes a field and, in the same transaction, its values from every game and copy. It
// returns how many games and copies lost a value.
func (s *Service) Delete(ctx context.Context, id string) (games, copies int, err error) {
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, copies = 0, 0

		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := set.Remove(id); err != nil {
			return err
		}

		list, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		for _, g := range list {
			gameHad, n := g.RemoveFieldValues(id)
			if !gameHad && n == 0 {
				continue
			}

			if gameHad {
				games++
			}

			copies += n

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}
		}

		return s.defs.SaveFields(ctx, set)
	})

	return games, copies, err
}

// RemoveChoice removes a list value and, in the same transaction, clears it from every game and
// copy, or replaces it with mergeInto.
func (s *Service) RemoveChoice(ctx context.Context, fieldID, choiceID, mergeInto string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := set.RemoveChoice(fieldID, choiceID, mergeInto); err != nil {
			return err
		}

		list, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		for _, g := range list {
			if !g.ReplaceChoice(fieldID, choiceID, mergeInto) {
				continue
			}

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}
		}

		return s.defs.SaveFields(ctx, set)
	})
}

// edit loads the definitions, applies fn and saves them, in one transaction.
func (s *Service) edit(ctx context.Context, fn func(*field.Set) error) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		set, err := s.defs.Fields(ctx)
		if err != nil {
			return err
		}

		if err := fn(set); err != nil {
			return err
		}

		return s.defs.SaveFields(ctx, set)
	})
}
