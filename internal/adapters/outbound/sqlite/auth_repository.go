package sqlite

import (
	"context"
	"database/sql"
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

// user reads the first user the clause selects. The clause follows "FROM users" (a WHERE, an
// ORDER BY…), so each lookup writes only the part that differs.
func (r *AuthRepository) user(ctx context.Context, clause string, args ...any) (auth.User, error) {
	var (
		u                auth.User
		created, updated string
	)

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, updated_at FROM users `+clause, args...).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return u, auth.ErrNotFound
	}

	u.CreatedAt, u.UpdatedAt = parseTime(created), parseTime(updated)

	return u, err
}

// UserByUsername returns the user with the given user name.
func (r *AuthRepository) UserByUsername(ctx context.Context, username string) (auth.User, error) {
	return r.user(ctx, `WHERE username = ?`, username)
}

// UserByID returns the user with the given id.
func (r *AuthRepository) UserByID(ctx context.Context, id string) (auth.User, error) {
	return r.user(ctx, `WHERE id = ?`, id)
}

// FirstUser returns the oldest user: Game Vault has one user.
func (r *AuthRepository) FirstUser(ctx context.Context) (auth.User, error) {
	return r.user(ctx, `ORDER BY created_at LIMIT 1`)
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

// tokenRow is a *sql.Row or *sql.Rows positioned on an access token.
type tokenRow interface {
	// Scan copies the row's columns into dest.
	Scan(dest ...any) error
}

const accessTokenColumns = `id, name, token_hash, created_at, last_used_at`

func scanAccessToken(row tokenRow) (auth.AccessToken, error) {
	var (
		t             auth.AccessToken
		created, used string
	)

	err := row.Scan(&t.ID, &t.Name, &t.Hash, &created, &used)
	t.CreatedAt, t.LastUsedAt = parseTime(created), parseTime(used)

	return t, err
}

// AccessTokens returns every access token, newest first.
func (r *AuthRepository) AccessTokens(ctx context.Context) ([]auth.AccessToken, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx, `SELECT `+accessTokenColumns+` FROM access_tokens ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auth.AccessToken

	for rows.Next() {
		t, err := scanAccessToken(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, t)
	}

	return out, rows.Err()
}

// AccessTokenByHash returns the access token whose secret has that hash, or auth.ErrNotFound.
func (r *AuthRepository) AccessTokenByHash(ctx context.Context, hash string) (auth.AccessToken, error) {
	t, err := scanAccessToken(r.db.conn(ctx).QueryRowContext(ctx, `SELECT `+accessTokenColumns+` FROM access_tokens WHERE token_hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return t, auth.ErrNotFound
	}

	return t, err
}

// SaveAccessToken creates an access token.
func (r *AuthRepository) SaveAccessToken(ctx context.Context, t auth.AccessToken) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `INSERT INTO access_tokens (`+accessTokenColumns+`) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Hash, formatTime(t.CreatedAt), optionalTime(t.LastUsedAt))

	return err
}

// TouchAccessToken records when an access token was last used.
func (r *AuthRepository) TouchAccessToken(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.conn(ctx).ExecContext(ctx, `UPDATE access_tokens SET last_used_at = ? WHERE id = ?`, formatTime(at), id)
	return err
}

// DeleteAccessToken removes an access token, or returns auth.ErrNotFound when there is none with that id.
func (r *AuthRepository) DeleteAccessToken(ctx context.Context, id string) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM access_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return auth.ErrNotFound
	}

	return nil
}

const keyAuth = "auth"

// authSettingsJSON is how the security settings are stored.
type authSettingsJSON struct {
	Authentication        string   `json:"authentication,omitempty"`
	TrustedNetworks       []string `json:"trustedNetworks,omitempty"`
	CertificateValidation string   `json:"certificateValidation,omitempty"`
}

// Auth implements auth.SettingsRepository.
func (r *SettingsRepository) Auth(ctx context.Context) (auth.Settings, error) {
	var v authSettingsJSON

	ok, err := r.db.getSetting(ctx, keyAuth, &v)
	if err != nil {
		return auth.Settings{}, err
	}

	if !ok {
		return r.defaultAuth(), nil
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
	return r.db.putSetting(ctx, keyAuth, authSettingsJSON{
		Authentication:        string(s.Authentication),
		TrustedNetworks:       s.TrustedNetworks,
		CertificateValidation: string(s.CertificateValidation),
	})
}
