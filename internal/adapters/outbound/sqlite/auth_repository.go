package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"gamevault/internal/domain/auth"
)

// AuthRepository implements auth.Repository.
type AuthRepository struct{ db *DB }

// NewAuthRepository returns the repository backed by db.
func NewAuthRepository(db *DB) *AuthRepository { return &AuthRepository{db: db} }

// CountUsers returns how many users exist.
func (r *AuthRepository) CountUsers(ctx context.Context) (int, error) {
	var n int

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)

	return n, err
}

func (r *AuthRepository) user(ctx context.Context, where string, arg any) (auth.User, error) {
	var (
		u                auth.User
		created, updated string
	)

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, updated_at FROM users WHERE `+where, arg).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return u, auth.ErrNotFound
	}

	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)

	return u, err
}

// UserByUsername returns the user with the given user name.
func (r *AuthRepository) UserByUsername(ctx context.Context, username string) (auth.User, error) {
	return r.user(ctx, `username = ?`, username)
}

// UserByID returns the user with the given id.
func (r *AuthRepository) UserByID(ctx context.Context, id string) (auth.User, error) {
	return r.user(ctx, `id = ?`, id)
}

// FirstUser returns the oldest user: Game Vault has one user.
func (r *AuthRepository) FirstUser(ctx context.Context) (auth.User, error) {
	return r.user(ctx, `id = (SELECT id FROM users ORDER BY created_at LIMIT 1) AND ? = 1`, 1)
}

// SaveUser inserts or updates a user.
func (r *AuthRepository) SaveUser(ctx context.Context, u auth.User) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET username = excluded.username, password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
		u.ID, u.Username, u.PasswordHash, formatTime(u.CreatedAt), formatTime(u.UpdatedAt))

	return err
}

// Session returns the session with the given token hash.
func (r *AuthRepository) Session(ctx context.Context, tokenHash string) (auth.Session, error) {
	var (
		s                      auth.Session
		created, expires, seen string
	)

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT token_hash, user_id, created_at, expires_at, last_seen_at, user_agent FROM sessions WHERE token_hash = ?`, tokenHash).
		Scan(&s.TokenHash, &s.UserID, &created, &expires, &seen, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return s, auth.ErrNotFound
	}

	s.CreatedAt, s.ExpiresAt, s.LastSeenAt = parseTime(created), parseTime(expires), parseTime(seen)

	return s, err
}

// SaveSession inserts or updates a session.
func (r *AuthRepository) SaveSession(ctx context.Context, s auth.Session) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen_at, user_agent)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(token_hash) DO UPDATE SET last_seen_at = excluded.last_seen_at, expires_at = excluded.expires_at`,
		s.TokenHash, s.UserID, formatTime(s.CreatedAt), formatTime(s.ExpiresAt), formatTime(s.LastSeenAt), s.UserAgent)

	return err
}

// DeleteSession removes the session with the given token hash.
func (r *AuthRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserSessions removes every session of a user.
func (r *AuthRepository) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// DeleteExpiredSessions removes the sessions that expired before now.
func (r *AuthRepository) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(now))
	return err
}

const keyAuth = "auth"

// authSettingsJSON is how the security settings are stored. The legacy fields come from versions
// that trusted Tailscale identities; they are read once to keep the same behavior and dropped.
type authSettingsJSON struct {
	Authentication        string   `json:"authentication,omitempty"`
	TrustedNetworks       []string `json:"trustedNetworks,omitempty"`
	CertificateValidation string   `json:"certificateValidation,omitempty"`

	LegacyLocalBypass *bool `json:"localBypass,omitempty"`
}

// Auth implements auth.SettingsRepository.
func (r *SettingsRepository) Auth(ctx context.Context) (auth.Settings, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyAuth).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r.defaultAuth(), nil
	}

	if err != nil {
		return auth.Settings{}, err
	}

	var v authSettingsJSON
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return auth.Settings{}, err
	}

	if v.Authentication == "" && v.LegacyLocalBypass != nil {
		s := r.defaultAuth()
		if !*v.LegacyLocalBypass {
			s.Authentication = auth.AuthRequired
		}

		return s, nil
	}

	return auth.Settings{
		Authentication:        auth.Authentication(v.Authentication),
		TrustedNetworks:       v.TrustedNetworks,
		CertificateValidation: auth.CertificateValidation(v.CertificateValidation),
	}.Normalize()
}

// defaultAuth is auth.DefaultSettings with the configured trusted networks, if any.
func (r *SettingsRepository) defaultAuth() auth.Settings {
	s := auth.DefaultSettings()
	if len(r.DefaultTrustedNetworks) > 0 {
		s.TrustedNetworks = slices.Clone(r.DefaultTrustedNetworks)
	}

	return s
}

// SaveAuth stores the authentication settings.
func (r *SettingsRepository) SaveAuth(ctx context.Context, s auth.Settings) error {
	b, err := json.Marshal(authSettingsJSON{
		Authentication:        string(s.Authentication),
		TrustedNetworks:       s.TrustedNetworks,
		CertificateValidation: string(s.CertificateValidation),
	})
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, keyAuth, string(b), formatTime(time.Now()))

	return err
}
