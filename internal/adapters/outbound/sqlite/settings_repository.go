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
type SettingsRepository struct{ db *DB }

func NewSettingsRepository(db *DB) *SettingsRepository { return &SettingsRepository{db: db} }

const keyLogging = "logging"

type loggingJSON struct {
	Level         string `json:"level"`
	MaxFileSizeMB int    `json:"maxFileSizeMb"`
	MaxFiles      int    `json:"maxFiles"`
}

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
