package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gamevault/internal/domain/field"
)

var _ field.Repository = (*SettingsRepository)(nil)

const keyFields = "fields"

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

	set, err := decodeFields(raw)
	if err != nil {
		return nil, fmt.Errorf("reading custom fields: %w", err)
	}

	return set, nil
}

// SaveFields stores the custom field definitions.
func (r *SettingsRepository) SaveFields(ctx context.Context, s *field.Set) error {
	raw, err := encodeFields(s)
	if err != nil {
		return fmt.Errorf("saving custom fields: %w", err)
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keyFields, raw, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("saving custom fields: %w", err)
	}

	return nil
}
