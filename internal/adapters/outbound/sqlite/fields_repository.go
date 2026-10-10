package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

var _ field.Repository = (*SettingsRepository)(nil)

const (
	keyFields = "fields"

	// fieldsDocVersion is the version of the stored custom field definitions.
	fieldsDocVersion = 1
)

// fieldsDoc is the stored form of a field.Set. Field names never change once released.
type fieldsDoc struct {
	V      int        `json:"v"`
	Fields []fieldDoc `json:"fields"`
}

// fieldDoc is the stored form of a field.Definition.
type fieldDoc struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Scope    string      `json:"scope"`
	Kinds    []string    `json:"kinds,omitempty"`
	Decimals int         `json:"decimals,omitempty"`
	Unit     string      `json:"unit,omitempty"`
	Currency string      `json:"currency,omitempty"`
	Choices  []choiceDoc `json:"choices,omitempty"`
}

// choiceDoc is the stored form of a field.Choice.
type choiceDoc struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Fields returns the custom field definitions, an empty set when none were saved.
func (r *SettingsRepository) Fields(ctx context.Context) (*field.Set, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyFields).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return field.NewSet(nil), nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading custom fields: %w", err)
	}

	var doc fieldsDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("reading custom fields: %w", err)
	}

	if doc.V != fieldsDocVersion {
		return nil, fmt.Errorf("custom fields document version %d: %w", doc.V, errNewerDocument)
	}

	defs := make([]field.Definition, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		d := field.Definition{
			ID:       f.ID,
			Name:     f.Name,
			Type:     field.Type(f.Type),
			Scope:    field.Scope(f.Scope),
			Decimals: f.Decimals,
			Unit:     f.Unit,
			Currency: f.Currency,
		}

		for _, k := range f.Kinds {
			d.Kinds = append(d.Kinds, game.Kind(k))
		}

		for _, c := range f.Choices {
			d.Choices = append(d.Choices, field.Choice{
				ID:   c.ID,
				Name: c.Name,
			})
		}

		defs = append(defs, d)
	}

	return field.NewSet(defs), nil
}

// SaveFields stores the custom field definitions.
func (r *SettingsRepository) SaveFields(ctx context.Context, s *field.Set) error {
	defs := s.Definitions()
	doc := fieldsDoc{
		V:      fieldsDocVersion,
		Fields: make([]fieldDoc, 0, len(defs)),
	}

	for _, d := range defs {
		f := fieldDoc{
			ID:       d.ID,
			Name:     d.Name,
			Type:     string(d.Type),
			Scope:    string(d.Scope),
			Decimals: d.Decimals,
			Unit:     d.Unit,
			Currency: d.Currency,
		}

		for _, k := range d.Kinds {
			f.Kinds = append(f.Kinds, string(k))
		}

		for _, c := range d.Choices {
			f.Choices = append(f.Choices, choiceDoc{
				ID:   c.ID,
				Name: c.Name,
			})
		}

		doc.Fields = append(doc.Fields, f)
	}

	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("saving custom fields: %w", err)
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keyFields, string(b), formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("saving custom fields: %w", err)
	}

	return nil
}
