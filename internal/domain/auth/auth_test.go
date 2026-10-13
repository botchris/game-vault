package auth

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/errkind"
)

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		problem  string // a part of the error's message; empty when the password is accepted
	}{
		{"eight letters", "abcdefgh", ""},
		{"seven letters", "abcdefg", "at least 8 characters"},
		{"eight accented letters, 16 bytes", strings.Repeat("ñ", 8), ""},
		{"five accented letters are five characters, not ten", strings.Repeat("ñ", 5), "at least 8 characters"},
		{"72 letters, bcrypt's limit", strings.Repeat("a", 72), ""},
		{"73 letters", strings.Repeat("a", 73), "at most 72 characters"},
		{"36 accented letters, 72 bytes", strings.Repeat("ñ", 36), ""},
		{"37 accented letters, 74 bytes", strings.Repeat("ñ", 37), "fewer with accented letters or emoji"},
	}
	for _, c := range cases {
		err := ValidatePassword(c.password)
		if c.problem == "" {
			assert.NoError(t, err, c.name)
			continue
		}

		var v *errkind.ValidationError
		if assert.ErrorAs(t, err, &v, c.name) {
			assert.Contains(t, err.Error(), c.problem, c.name)
		}
	}
}

func TestNormalize(t *testing.T) {
	s, err := Settings{TrustedNetworks: []string{" 192.168.1.77/24 ", "10.0.0.5", "192.168.1.0/24", "", "fd00::/8"}}.Normalize()
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.5/32", "192.168.1.0/24", "fd00::/8"}, s.TrustedNetworks)
	assert.Equal(t, AuthTrustedNetworks, s.Authentication, "the default authentication")
	assert.Equal(t, CertsEnabled, s.CertificateValidation, "the default certificate validation")

	for _, bad := range []Settings{
		{TrustedNetworks: []string{"home"}},
		{TrustedNetworks: []string{"0.0.0.0/0"}},
		{Authentication: "sometimes"},
		{CertificateValidation: "maybe"},
	} {
		_, err := bad.Normalize()

		var v *errkind.ValidationError
		assert.ErrorAs(t, err, &v, "%+v should be rejected", bad)
	}
}

func TestTrusts(t *testing.T) {
	s := DefaultSettings()
	s.TrustedNetworks = append(s.TrustedNetworks, "192.168.1.0/24")
	ip := netip.MustParseAddr

	cases := []struct {
		name string
		r    Request
		want bool
	}{
		{"this computer", Request{
			ClientIP:      ip("127.0.0.1"),
			HostIsAddress: true,
		}, true},
		{"this computer over IPv6", Request{
			ClientIP:      ip("::1"),
			HostIsAddress: true,
		}, true},
		{"IPv4-mapped address", Request{
			ClientIP:      ip("::ffff:192.168.1.20"),
			HostIsAddress: true,
		}, true},
		{"home network", Request{
			ClientIP:      ip("192.168.1.20"),
			HostIsAddress: true,
		}, true},
		{"another network", Request{
			ClientIP:      ip("192.168.2.20"),
			HostIsAddress: true,
		}, false},
		{"through a proxy", Request{
			ClientIP:      ip("127.0.0.1"),
			HostIsAddress: true,
			Forwarded:     true,
		}, false},
		{"DNS rebinding (a site's own name pointing here)", Request{ClientIP: ip("127.0.0.1")}, false},
		{"no address", Request{HostIsAddress: true}, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, s.Trusts(c.r), c.name)
	}
}

func TestDefaultTrustedNetworks_areReadOnly(t *testing.T) {
	got := DefaultTrustedNetworks()
	got[0] = "0.0.0.0/0"

	assert.Equal(t, []string{"127.0.0.0/8", "::1/128"}, DefaultTrustedNetworks(), "after a caller changed its copy")
	assert.Equal(t, []string{"127.0.0.0/8", "::1/128"}, DefaultSettings().TrustedNetworks, "this computer only")
}
