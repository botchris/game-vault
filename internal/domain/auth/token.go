package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TokenPrefix starts every access token, so a leaked one is easy to recognize and to search for.
const TokenPrefix = "gvt_"

// TokenBytes is how many random bytes an access token carries.
const TokenBytes = 32

// maxTokenName bounds a token's name, in characters.
const maxTokenName = 60

// AccessToken lets a program, such as the browser extension, look up what the user owns and
// nothing else (see MethodToken). Only a hash of its secret is stored: the secret is shown once,
// when the token is created.
type AccessToken struct {
	ID string

	// Name says where the token is used ("Extension, laptop").
	Name string

	// Hash is HashToken of the secret.
	Hash      string
	CreatedAt time.Time

	// LastUsedAt is when the token last authenticated a request, kept to the minute; zero when never.
	LastUsedAt time.Time
}

// NewAccessToken creates a token named name from random bytes (at least TokenBytes, from a
// cryptographic source) and returns it with its secret: TokenPrefix and the bytes in unpadded
// base64url.
func NewAccessToken(name string, random []byte, now time.Time) (AccessToken, string, error) {
	name, err := normalizeTokenName(name)
	if err != nil {
		return AccessToken{}, "", err
	}

	if len(random) < TokenBytes {
		return AccessToken{}, "", fmt.Errorf("an access token needs %d random bytes, got %d", TokenBytes, len(random))
	}

	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(random)

	return AccessToken{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Name:      name,
		Hash:      HashToken(secret),
		CreatedAt: now,
	}, secret, nil
}

// HashToken returns the stored form of a session or access token: its SHA-256, in hex. Tokens are
// long and random, so a fast hash is enough (passwords need bcrypt).
func HashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// IsAccessToken reports whether a bearer credential is one of Game Vault's access tokens. Others,
// such as the token an authenticating proxy in front of Game Vault forwards, are not Game Vault's
// to check.
func IsAccessToken(s string) bool {
	return len(s) > len(TokenPrefix) && strings.HasPrefix(s, TokenPrefix)
}

func normalizeTokenName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxTokenName || strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalid("give the token a name of at most %d characters, such as \"Extension, laptop\"", maxTokenName)
	}

	return name, nil
}
