// Package auth holds access control: who may use Game Vault and how they are recognized.
//
// There is one user with a password. Authentication is either required for everyone, or not
// required for requests from trusted networks (by default only this computer). Requests that
// come through a proxy, or that name the server by a host name instead of an address, never count
// as coming from a trusted network.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Errors returned by the auth use cases. Callers match them with errors.Is.
var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrBadCredentials  = errors.New("wrong username or password")
	ErrNotFound        = errors.New("not found")
	ErrSetupDone       = errors.New("a user already exists")
	ErrSetupNotTrusted = errors.New("the user can only be created from a trusted network (by default, the computer running Game Vault; in Docker, your local network)")
	ErrTooManyAttempts = errors.New("too many failed attempts; wait a few minutes")
	ErrLockout         = &ValidationError{"create a user before making authentication required, or you would lock yourself out"}
)

// ValidationError reports input that breaks a rule.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{fmt.Sprintf(format, args...)}
}

// Request is what is known about who sent an HTTP request. Adapters fill it in.
type Request struct {
	// ClientIP is the TCP peer's address.
	ClientIP netip.Addr
	// HostIsAddress: the Host header is an IP address or "localhost", not a name. A trusted
	// network only counts in that case, so a malicious website cannot reach Game Vault through
	// DNS rebinding (its own name pointing at this machine) and use the network's trust.
	HostIsAddress bool
	// Forwarded: the request carries proxy headers (X-Forwarded-For, Forwarded, X-Real-IP).
	// Behind a proxy the peer is the proxy, not the caller, so the network is not trusted.
	Forwarded    bool
	SessionToken string
	HTTPS        bool
	UserAgent    string
}

// Authentication says when a password is needed.
type Authentication string

const (
	// AuthRequired means everyone signs in.
	AuthRequired Authentication = "required"
	// AuthTrustedNetworks lets requests from trusted networks get in without signing in.
	AuthTrustedNetworks Authentication = "trusted_networks"
)

// CertificateValidation says when the HTTPS certificates of the services Game Vault connects to
// (stores, providers) are checked.
type CertificateValidation string

// Values of CertificateValidation, from strict to fully off.
const (
	CertsEnabled       CertificateValidation = "enabled"
	CertsLocalDisabled CertificateValidation = "local_disabled"
	CertsDisabled      CertificateValidation = "disabled"
)

// Settings controls access to Game Vault and its outgoing connections.
type Settings struct {
	Authentication Authentication
	// TrustedNetworks are CIDR prefixes ("192.168.1.0/24") or single addresses.
	TrustedNetworks       []string
	CertificateValidation CertificateValidation
}

// DefaultTrustedNetworks is this computer only.
var DefaultTrustedNetworks = []string{"127.0.0.0/8", "::1/128"}

// DefaultSettings returns the out-of-the-box settings: no password needed on this computer,
// certificates checked.
func DefaultSettings() Settings {
	return Settings{
		Authentication: AuthTrustedNetworks, TrustedNetworks: slices.Clone(DefaultTrustedNetworks),
		CertificateValidation: CertsEnabled,
	}
}

// parsePrefix accepts "10.0.0.0/8", "192.168.1.20" (one address) and IPv6 forms.
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}

	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}

	return netip.PrefixFrom(a, a.BitLen()), nil
}

// Normalize validates the settings: known modes, valid networks (canonical, unique, sorted).
func (s Settings) Normalize() (Settings, error) {
	switch s.Authentication {
	case AuthRequired, AuthTrustedNetworks:
	case "":
		s.Authentication = AuthTrustedNetworks
	default:
		return s, invalid("unknown authentication mode %q", s.Authentication)
	}

	switch s.CertificateValidation {
	case CertsEnabled, CertsLocalDisabled, CertsDisabled:
	case "":
		s.CertificateValidation = CertsEnabled
	default:
		return s, invalid("unknown certificate validation %q", s.CertificateValidation)
	}

	var nets []string

	for _, n := range s.TrustedNetworks {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}

		p, err := parsePrefix(n)
		if err != nil {
			return s, invalid("%q is not a network (e.g. 192.168.1.0/24) or an address", n)
		}

		if p.Bits() == 0 {
			return s, invalid("%q would trust every address on the internet", n)
		}

		if c := p.String(); !slices.Contains(nets, c) {
			nets = append(nets, c)
		}
	}

	slices.Sort(nets)
	s.TrustedNetworks = nets

	return s, nil
}

// Trusts reports whether the request comes straight from a trusted network.
func (s Settings) Trusts(r Request) bool {
	return !r.Forwarded && r.HostIsAddress && s.Contains(r.ClientIP)
}

// Contains reports whether ip is in one of the trusted networks.
func (s Settings) Contains(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}

	ip = ip.Unmap()
	for _, n := range s.TrustedNetworks {
		if p, err := parsePrefix(n); err == nil && p.Contains(ip) {
			return true
		}
	}

	return false
}

// Method says how a principal was recognized.
type Method string

// Values of Method.
const (
	MethodTrusted Method = "trusted" // from a trusted network, no sign-in
	MethodSession Method = "session" // signed in with the password
)

// Principal is an authenticated caller.
type Principal struct {
	Method Method
	// Name is the username (session) or "trusted network".
	Name   string
	UserID string // session only
}

// User is the account for username/password login.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

var reUsername = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

// ValidateCredentials checks a new username and password.
func ValidateCredentials(username, password string) error {
	if !reUsername.MatchString(username) {
		return invalid("username must be 3-32 letters, digits, dots, dashes or underscores")
	}

	return ValidatePassword(password)
}

// ValidatePassword checks a new password.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return invalid("password must have at least 8 characters")
	}

	if len(password) > 72 {
		return invalid("password must have at most 72 characters")
	}

	return nil
}

// NewUser creates a user from an already hashed password.
func NewUser(username, hash string, now time.Time) User {
	return User{ID: uuid.Must(uuid.NewV7()).String(), Username: username, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
}

// Session is a logged-in browser. Only a hash of the token is stored.
type Session struct {
	TokenHash  string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
}

// Expired reports whether the session can no longer be used.
func (s Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// Repository is the persistence port for users and sessions.
type Repository interface {
	CountUsers(ctx context.Context) (int, error)
	UserByUsername(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)
	FirstUser(ctx context.Context) (User, error)
	SaveUser(ctx context.Context, u User) error

	Session(ctx context.Context, tokenHash string) (Session, error)
	SaveSession(ctx context.Context, s Session) error
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error
}

// SettingsRepository is the persistence port for the security settings.
type SettingsRepository interface {
	Auth(ctx context.Context) (Settings, error) // DefaultSettings when none saved
	SaveAuth(ctx context.Context, s Settings) error
}
