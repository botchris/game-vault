package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
)

func TestNewAccessToken(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	random := bytes.Repeat([]byte{0xA5}, TokenBytes)

	t.Run("GIVEN a name and 32 random bytes", func(t *testing.T) {
		tok, secret, err := NewAccessToken("  Extension,   laptop ", random, now)
		require.NoError(t, err)

		t.Run("THEN the secret starts with gvt_ and carries every random byte", func(t *testing.T) {
			assert.True(t, strings.HasPrefix(secret, TokenPrefix))
			assert.Len(t, secret, len(TokenPrefix)+43) // 32 bytes in unpadded base64url
			assert.True(t, IsAccessToken(secret))
		})

		t.Run("AND only its hash is kept", func(t *testing.T) {
			assert.Equal(t, HashToken(secret), tok.Hash)
			assert.Len(t, tok.Hash, 64)
		})

		t.Run("AND the name is tidied, the id set, and it was never used", func(t *testing.T) {
			assert.Equal(t, "Extension, laptop", tok.Name)
			assert.NotEmpty(t, tok.ID)
			assert.Equal(t, now, tok.CreatedAt)
			assert.True(t, tok.LastUsedAt.IsZero())
		})
	})

	t.Run("GIVEN two tokens made from different random bytes", func(t *testing.T) {
		a, sa, err := NewAccessToken("a", bytes.Repeat([]byte{1}, TokenBytes), now)
		require.NoError(t, err)
		b, sb, err := NewAccessToken("b", bytes.Repeat([]byte{2}, TokenBytes), now)
		require.NoError(t, err)

		t.Run("THEN their secrets, hashes and ids differ", func(t *testing.T) {
			assert.NotEqual(t, sa, sb)
			assert.NotEqual(t, a.Hash, b.Hash)
			assert.NotEqual(t, a.ID, b.ID)
		})
	})

	t.Run("GIVEN bad input", func(t *testing.T) {
		t.Run("THEN names that are empty, too long or with control characters are refused", func(t *testing.T) {
			for _, name := range []string{"", "   ", strings.Repeat("x", 61), "lap\x00top"} {
				_, _, err := NewAccessToken(name, random, now)

				var ve *errkind.ValidationError
				assert.ErrorAs(t, err, &ve, "%q", name)
			}
		})

		t.Run("AND fewer than 32 random bytes are refused", func(t *testing.T) {
			_, _, err := NewAccessToken("short", random[:16], now)
			assert.Error(t, err)
		})
	})
}

func TestIsAccessToken(t *testing.T) {
	for s, want := range map[string]bool{
		"gvt_abc":                      true,
		"gvt_":                         false,
		"GVT_abc":                      false,
		"eyJhbGciOiJSUzI1NiJ9.e30.sig": false, // a token an authenticating proxy forwards
		"YWRtaW46c2VjcmV0":             false,
		"":                             false,
	} {
		assert.Equal(t, want, IsAccessToken(s), "%q", s)
	}
}
