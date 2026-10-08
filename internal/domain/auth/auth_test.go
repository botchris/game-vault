package auth

import (
	"errors"
	"net/netip"
	"slices"
	"testing"
)

func TestNormalize(t *testing.T) {
	s, err := Settings{TrustedNetworks: []string{" 192.168.1.77/24 ", "10.0.0.5", "192.168.1.0/24", "", "fd00::/8"}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"10.0.0.5/32", "192.168.1.0/24", "fd00::/8"}; !slices.Equal(s.TrustedNetworks, want) {
		t.Fatalf("networks = %v, want %v", s.TrustedNetworks, want)
	}

	if s.Authentication != AuthTrustedNetworks || s.CertificateValidation != CertsEnabled {
		t.Fatalf("defaults: %+v", s)
	}

	for _, bad := range []Settings{
		{TrustedNetworks: []string{"home"}},
		{TrustedNetworks: []string{"0.0.0.0/0"}},
		{Authentication: "sometimes"},
		{CertificateValidation: "maybe"},
	} {
		var v *ValidationError
		if _, err := bad.Normalize(); !errors.As(err, &v) {
			t.Errorf("%+v should be rejected, got %v", bad, err)
		}
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
		{"this computer", Request{ClientIP: ip("127.0.0.1"), HostIsAddress: true}, true},
		{"this computer over IPv6", Request{ClientIP: ip("::1"), HostIsAddress: true}, true},
		{"IPv4-mapped address", Request{ClientIP: ip("::ffff:192.168.1.20"), HostIsAddress: true}, true},
		{"home network", Request{ClientIP: ip("192.168.1.20"), HostIsAddress: true}, true},
		{"another network", Request{ClientIP: ip("192.168.2.20"), HostIsAddress: true}, false},
		{"through a proxy", Request{ClientIP: ip("127.0.0.1"), HostIsAddress: true, Forwarded: true}, false},
		{"DNS rebinding (a site's own name pointing here)", Request{ClientIP: ip("127.0.0.1")}, false},
		{"no address", Request{HostIsAddress: true}, false},
	}
	for _, c := range cases {
		if got := s.Trusts(c.r); got != c.want {
			t.Errorf("%s: Trusts = %v, want %v", c.name, got, c.want)
		}
	}
}
