// Package auth implements the access control use cases.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"
	gosync "sync"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/auth"
)

// Hasher is the port that hashes and verifies passwords.
type Hasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

// CertificatePolicy is the port that applies the certificate validation setting to Game Vault's
// outgoing HTTPS connections (stores, providers).
type CertificatePolicy interface {
	SetCertificateValidation(auth.CertificateValidation)
}

const (
	sessionTTL    = 30 * 24 * time.Hour
	touchEvery    = time.Hour // how often a session's last-seen time is written
	maxFailures   = 10        // failed logins allowed per window
	failureWindow = 15 * time.Minute
)

// Status describes the caller's situation, for the UI to decide what to show.
type Status struct {
	Principal *auth.Principal
	// SetupRequired: no user exists yet.
	SetupRequired bool
	// CanSetup: no user exists and the caller is on a trusted network.
	CanSetup bool
	// Trusted: the request comes straight from a trusted network.
	Trusted bool
}

// Service exposes the auth use cases.
type Service struct {
	repo     auth.Repository
	settings auth.SettingsRepository
	hash     Hasher
	certs    CertificatePolicy
	now      port.Clock
	log      *slog.Logger

	// dummyHash is verified when the username does not exist, so the answer takes as long as for
	// a real user and does not reveal which usernames exist.
	dummyHash string

	mu       gosync.Mutex
	failures []time.Time
}

// NewService builds the service. It takes the clock and the hasher as parameters so tests control time
// and avoid slow password hashing.
func NewService(repo auth.Repository, settings auth.SettingsRepository, hash Hasher, certs CertificatePolicy, now port.Clock, log *slog.Logger) *Service {
	dummy, _ := hash.Hash("timing-equalizer-not-a-password")
	return &Service{repo: repo, settings: settings, hash: hash, certs: certs, now: now, log: log, dummyHash: dummy}
}

// Init applies the saved settings that act outside requests (certificate validation). Call it on
// start-up.
func (s *Service) Init(ctx context.Context) error {
	settings, err := s.settings.Auth(ctx)
	if err != nil {
		return err
	}

	s.certs.SetCertificateValidation(settings.CertificateValidation)

	return nil
}

// ResetAuthentication makes authentication not required on trusted networks again: the way back
// in when the password is forgotten while authentication is required (a start-up flag).
func (s *Service) ResetAuthentication(ctx context.Context) error {
	settings, err := s.settings.Auth(ctx)
	if err != nil {
		return err
	}

	settings.Authentication = auth.AuthTrustedNetworks
	if len(settings.TrustedNetworks) == 0 {
		settings.TrustedNetworks = auth.DefaultTrustedNetworks
	}

	s.log.Warn("authentication reset: no password needed from trusted networks", "trusted_networks", settings.TrustedNetworks)

	return s.settings.SaveAuth(ctx, settings)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Authenticate recognizes the caller: a valid session, or a trusted network when authentication
// is not required there.
func (s *Service) Authenticate(ctx context.Context, r auth.Request) (auth.Principal, error) {
	if p, ok := s.fromSession(ctx, r); ok {
		return p, nil
	}

	settings, err := s.settings.Auth(ctx)
	if err != nil {
		return auth.Principal{}, err
	}

	if settings.Authentication == auth.AuthTrustedNetworks && settings.Trusts(r) {
		return auth.Principal{Method: auth.MethodTrusted, Name: "trusted network"}, nil
	}

	return auth.Principal{}, auth.ErrUnauthenticated
}

func (s *Service) fromSession(ctx context.Context, r auth.Request) (auth.Principal, bool) {
	if r.SessionToken == "" {
		return auth.Principal{}, false
	}

	sess, err := s.repo.Session(ctx, hashToken(r.SessionToken))

	now := s.now()
	if err != nil || sess.Expired(now) {
		return auth.Principal{}, false
	}

	u, err := s.repo.UserByID(ctx, sess.UserID)
	if err != nil {
		return auth.Principal{}, false
	}

	if now.Sub(sess.LastSeenAt) > touchEvery {
		sess.LastSeenAt = now
		if err := s.repo.SaveSession(ctx, sess); err != nil {
			s.log.Warn("touching session", "error", err)
		}
	}

	return auth.Principal{Method: auth.MethodSession, Name: u.Username, UserID: u.ID}, true
}

func (s *Service) trusted(ctx context.Context, r auth.Request) (bool, error) {
	settings, err := s.settings.Auth(ctx)
	if err != nil {
		return false, err
	}

	return settings.Trusts(r), nil
}

// Status reports who the caller is and whether first-time setup is possible.
func (s *Service) Status(ctx context.Context, r auth.Request) (Status, error) {
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return Status{}, err
	}

	trusted, err := s.trusted(ctx, r)
	if err != nil {
		return Status{}, err
	}

	st := Status{SetupRequired: n == 0, CanSetup: n == 0 && trusted, Trusted: trusted}
	if p, err := s.Authenticate(ctx, r); err == nil {
		st.Principal = &p
	} else if !errors.Is(err, auth.ErrUnauthenticated) {
		return st, err
	}

	return st, nil
}

// Setup creates the user. Only allowed from a trusted network, so nobody else can claim an empty
// installation. Returns a session token.
func (s *Service) Setup(ctx context.Context, r auth.Request, username, password string) (string, auth.Principal, error) {
	if ok, err := s.trusted(ctx, r); err != nil {
		return "", auth.Principal{}, err
	} else if !ok {
		return "", auth.Principal{}, auth.ErrSetupNotTrusted
	}

	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return "", auth.Principal{}, err
	}

	if n > 0 {
		return "", auth.Principal{}, auth.ErrSetupDone
	}

	if err := auth.ValidateCredentials(username, password); err != nil {
		return "", auth.Principal{}, err
	}

	hash, err := s.hash.Hash(password)
	if err != nil {
		return "", auth.Principal{}, err
	}

	u := auth.NewUser(username, hash, s.now())
	if err := s.repo.SaveUser(ctx, u); err != nil {
		return "", auth.Principal{}, err
	}

	s.log.Info("user created", "username", username)

	return s.newSession(ctx, u, r)
}

// Login checks credentials and opens a session. Failed attempts are throttled globally.
func (s *Service) Login(ctx context.Context, r auth.Request, username, password string) (string, auth.Principal, error) {
	if s.throttled() {
		return "", auth.Principal{}, auth.ErrTooManyAttempts
	}

	u, err := s.repo.UserByUsername(ctx, username)

	ok := err == nil && s.hash.Verify(u.PasswordHash, password)
	if errors.Is(err, auth.ErrNotFound) {
		s.hash.Verify(s.dummyHash, password)
	} else if err != nil {
		return "", auth.Principal{}, err
	}

	if !ok {
		s.recordFailure()
		s.log.Warn("failed login", "username", username, "client", r.ClientIP.String())

		return "", auth.Principal{}, auth.ErrBadCredentials
	}

	_ = s.repo.DeleteExpiredSessions(ctx, s.now())

	return s.newSession(ctx, u, r)
}

func (s *Service) newSession(ctx context.Context, u auth.User, r auth.Request) (string, auth.Principal, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", auth.Principal{}, err
	}

	token := base64.RawURLEncoding.EncodeToString(buf)
	now := s.now()
	err := s.repo.SaveSession(ctx, auth.Session{
		TokenHash: hashToken(token), UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(sessionTTL), LastSeenAt: now,
		UserAgent: truncate(r.UserAgent, 200),
	})

	return token, auth.Principal{Method: auth.MethodSession, Name: u.Username, UserID: u.ID}, err
}

// SessionTTL is how long a login lasts; adapters use it for the cookie lifetime.
func SessionTTL() time.Duration { return sessionTTL }

// Logout ends the session of the given token (no-op when there is none).
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	return s.repo.DeleteSession(ctx, hashToken(token))
}

// ChangePassword sets a new password, and optionally a new username. Signed-in users must give
// the current password; from a trusted network it can be reset without it (the recovery path if
// you forget it). Other sessions are closed.
func (s *Service) ChangePassword(ctx context.Context, p auth.Principal, username, current, next string) error {
	if err := auth.ValidatePassword(next); err != nil {
		return err
	}

	var (
		u   auth.User
		err error
	)

	switch p.Method {
	case auth.MethodSession:
		u, err = s.repo.UserByID(ctx, p.UserID)
		if err != nil {
			return err
		}

		if !s.hash.Verify(u.PasswordHash, current) {
			return auth.ErrBadCredentials
		}
	case auth.MethodTrusted:
		u, err = s.repo.FirstUser(ctx)
		if err != nil {
			return err
		}
	default:
		return auth.ErrUnauthenticated
	}

	if username = strings.TrimSpace(username); username != "" && username != u.Username {
		if err := auth.ValidateCredentials(username, next); err != nil {
			return err
		}

		u.Username = username
	}

	hash, err := s.hash.Hash(next)
	if err != nil {
		return err
	}

	u.PasswordHash, u.UpdatedAt = hash, s.now()
	if err := s.repo.SaveUser(ctx, u); err != nil {
		return err
	}

	return s.repo.DeleteUserSessions(ctx, u.ID)
}

// User returns the user (auth.ErrNotFound when none was created yet).
func (s *Service) User(ctx context.Context) (auth.User, error) { return s.repo.FirstUser(ctx) }

// Settings returns the security settings.
func (s *Service) Settings(ctx context.Context) (auth.Settings, error) { return s.settings.Auth(ctx) }

// UpdateSettings validates and saves the security settings. Requiring authentication needs a user
// to exist, so the owner cannot lock themselves out.
func (s *Service) UpdateSettings(ctx context.Context, in auth.Settings) (auth.Settings, error) {
	in, err := in.Normalize()
	if err != nil {
		return in, err
	}

	if in.Authentication == auth.AuthRequired {
		n, err := s.repo.CountUsers(ctx)
		if err != nil {
			return in, err
		}

		if n == 0 {
			return in, auth.ErrLockout
		}
	}

	if err := s.settings.SaveAuth(ctx, in); err != nil {
		return in, err
	}

	s.certs.SetCertificateValidation(in.CertificateValidation)
	s.log.Info("security settings saved", "authentication", in.Authentication, "trusted_networks", in.TrustedNetworks,
		"certificate_validation", in.CertificateValidation)

	return in, nil
}

func (s *Service) throttled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.now().Add(-failureWindow)
	s.failures = slices.DeleteFunc(s.failures, func(t time.Time) bool { return t.Before(cutoff) })

	return len(s.failures) >= maxFailures
}

func (s *Service) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures = append(s.failures, s.now())
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}

	return s
}
