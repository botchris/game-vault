package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/auth"
)

func TestAuthSettings(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	r := NewSettingsRepository(db)

	if s, err := r.Auth(ctx); err != nil || s.Authentication != auth.AuthTrustedNetworks || !slices.Equal(s.TrustedNetworks, auth.DefaultTrustedNetworks) {
		t.Fatalf("defaults: %+v %v", s, err)
	}

	want := auth.Settings{
		Authentication:        auth.AuthRequired,
		TrustedNetworks:       []string{"192.168.1.0/24"},
		CertificateValidation: auth.CertsLocalDisabled,
	}
	if err := r.SaveAuth(ctx, want); err != nil {
		t.Fatal(err)
	}

	if got, err := r.Auth(ctx); err != nil || got.Authentication != want.Authentication || got.CertificateValidation != want.CertificateValidation || !slices.Equal(got.TrustedNetworks, want.TrustedNetworks) {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	// Settings saved by versions that trusted Tailscale identities keep their behavior: no local
	// bypass means authentication required.
	for raw, mode := range map[string]auth.Authentication{
		`{"localBypass":false,"trustTailscale":true,"tailscaleLogins":["you@example.com"]}`: auth.AuthRequired,
		`{"localBypass":true,"trustTailscale":true,"tailscaleLogins":[]}`:                   auth.AuthTrustedNetworks,
	} {
		if _, err := db.conn(ctx).ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, raw, keyAuth); err != nil {
			t.Fatal(err)
		}

		got, err := r.Auth(ctx)
		if err != nil || got.Authentication != mode || !slices.Equal(got.TrustedNetworks, auth.DefaultTrustedNetworks) {
			t.Fatalf("legacy %s: %+v %v", raw, got, err)
		}
	}
}

func TestAuthSettings_defaultTrustedNetworks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a repository configured with the private networks, as in the Docker image", func(t *testing.T) {
		r := NewSettingsRepository(openTest(t))
		r.DefaultTrustedNetworks = []string{"10.0.0.0/8", "192.168.0.0/16"}

		t.Run("WHEN nothing has been saved yet", func(t *testing.T) {
			got, err := r.Auth(ctx)
			require.NoError(t, err)

			t.Run("THEN the configured networks are trusted instead of this computer", func(t *testing.T) {
				assert.Equal(t, auth.AuthTrustedNetworks, got.Authentication)
				assert.Equal(t, r.DefaultTrustedNetworks, got.TrustedNetworks)
			})

			t.Run("AND changing the result does not change the defaults", func(t *testing.T) {
				got.TrustedNetworks[0] = "127.0.0.1/32"

				assert.Equal(t, "10.0.0.0/8", r.DefaultTrustedNetworks[0])
			})
		})

		t.Run("WHEN the security settings are saved from the UI", func(t *testing.T) {
			saved := auth.Settings{
				Authentication:        auth.AuthTrustedNetworks,
				TrustedNetworks:       []string{"192.168.1.0/24"},
				CertificateValidation: auth.CertsEnabled,
			}
			require.NoError(t, r.SaveAuth(ctx, saved))

			got, err := r.Auth(ctx)
			require.NoError(t, err)

			t.Run("THEN the saved networks win over the configured ones", func(t *testing.T) {
				assert.Equal(t, saved.TrustedNetworks, got.TrustedNetworks)
			})
		})
	})
}
