package sqlite

import (
	"context"
	"slices"
	"testing"

	"gamevault/internal/domain/auth"
)

func TestAuthSettings(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	r := NewSettingsRepository(db)

	if s, err := r.Auth(ctx); err != nil || s.Authentication != auth.AuthTrustedNetworks || !slices.Equal(s.TrustedNetworks, auth.DefaultTrustedNetworks) {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	want := auth.Settings{Authentication: auth.AuthRequired, TrustedNetworks: []string{"192.168.1.0/24"}, CertificateValidation: auth.CertsLocalDisabled}
	if err := r.SaveAuth(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Auth(ctx); err != nil || got.Authentication != want.Authentication || got.CertificateValidation != want.CertificateValidation || !slices.Equal(got.TrustedNetworks, want.TrustedNetworks) {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	// Settings saved by versions that trusted Tailscale identities keep their behaviour: no local
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
