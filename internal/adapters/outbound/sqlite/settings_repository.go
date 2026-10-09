package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gamevault/internal/domain/settings"
)

// SettingsRepository implements settings.Repository with one JSON row per settings group.
type SettingsRepository struct {
	db *DB

	// DefaultTrustedNetworks replaces auth.DefaultTrustedNetworks until the security settings are
	// saved. A container sets it: there, requests never come from 127.0.0.1.
	DefaultTrustedNetworks []string
}

// NewSettingsRepository returns the repository backed by db.
func NewSettingsRepository(db *DB) *SettingsRepository { return &SettingsRepository{db: db} }

const keyLogging = "logging"

type loggingJSON struct {
	Level         string `json:"level"`
	MaxFileSizeMB int    `json:"maxFileSizeMb"`
	MaxFiles      int    `json:"maxFiles"`
}

// Logging returns the logging settings, or the defaults when none are stored.
func (r *SettingsRepository) Logging(ctx context.Context) (settings.Logging, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyLogging).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings.DefaultLogging(), nil
	}

	if err != nil {
		return settings.Logging{}, err
	}

	var v loggingJSON
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return settings.Logging{}, err
	}

	return settings.Logging(v), nil
}

// SaveLogging stores the logging settings.
func (r *SettingsRepository) SaveLogging(ctx context.Context, l settings.Logging) error {
	b, err := json.Marshal(loggingJSON(l))
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keyLogging, string(b), formatTime(time.Now()))

	return err
}

const keyPreferences = "preferences"

type preferencesJSON struct {
	Currency string `json:"currency,omitempty"`
}

// Preferences returns the saved preferences, empty when none were saved.
func (r *SettingsRepository) Preferences(ctx context.Context) (settings.Preferences, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyPreferences).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings.Preferences{}, nil
	}

	if err != nil {
		return settings.Preferences{}, err
	}

	var v preferencesJSON
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return settings.Preferences{}, err
	}

	return settings.Preferences(v), nil
}

// SavePreferences stores the preferences.
func (r *SettingsRepository) SavePreferences(ctx context.Context, p settings.Preferences) error {
	b, err := json.Marshal(preferencesJSON(p))
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		keyPreferences, string(b), formatTime(time.Now()))

	return err
}
