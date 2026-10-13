package sqlite

import (
	"context"
	"fmt"

	"gamevault/internal/domain/field"
)

var _ field.Repository = (*SettingsRepository)(nil)

const keyFields = "fields"

// Fields returns the custom field definitions, an empty set when none were saved.
func (r *SettingsRepository) Fields(ctx context.Context) (*field.Set, error) {
	raw, ok, err := r.db.readSetting(ctx, keyFields)
	if err != nil {
		return nil, fmt.Errorf("reading custom fields: %w", err)
	}

	if !ok {
		return field.NewSet(nil), nil
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

	if err := r.db.writeSetting(ctx, keyFields, raw); err != nil {
		return fmt.Errorf("saving custom fields: %w", err)
	}

	return nil
}
