package sqlite

import (
	"context"

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
	var v loggingJSON

	ok, err := r.db.getSetting(ctx, keyLogging, &v)
	if err != nil {
		return settings.Logging{}, err
	}

	if !ok {
		return settings.DefaultLogging(), nil
	}

	return settings.Logging(v), nil
}

// SaveLogging stores the logging settings.
func (r *SettingsRepository) SaveLogging(ctx context.Context, l settings.Logging) error {
	return r.db.putSetting(ctx, keyLogging, loggingJSON(l))
}

const keyPreferences = "preferences"

type preferencesJSON struct {
	Currency string `json:"currency,omitempty"`
}

// Preferences returns the saved preferences, empty when none were saved.
func (r *SettingsRepository) Preferences(ctx context.Context) (settings.Preferences, error) {
	var v preferencesJSON
	if _, err := r.db.getSetting(ctx, keyPreferences, &v); err != nil {
		return settings.Preferences{}, err
	}

	return settings.Preferences(v), nil
}

// SavePreferences stores the preferences.
func (r *SettingsRepository) SavePreferences(ctx context.Context, p settings.Preferences) error {
	return r.db.putSetting(ctx, keyPreferences, preferencesJSON(p))
}
