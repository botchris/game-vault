// Package tlspolicy applies the "certificate validation" security setting to Game Vault's
// outgoing HTTPS connections (stores, providers, images). Every HTTP client in the project uses
// Go's default transport, so installing the policy there covers all of them.
package tlspolicy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"gamevault/internal/domain/auth"
)

// Policy decides, per connection, whether the server certificate is checked.
type Policy struct {
	mode atomic.Value // auth.CertificateValidation
	base *tls.Config
}

// Install makes t dial TLS through the policy (certificates checked until told otherwise) and
// returns it, to be updated when the setting changes.
func Install(t *http.Transport) *Policy {
	p := &Policy{base: &tls.Config{MinVersion: tls.VersionTLS12}}
	if t.TLSClientConfig != nil {
		p.base = t.TLSClientConfig.Clone()
	}

	p.mode.Store(auth.CertsEnabled)

	dialer := &net.Dialer{}
	t.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}

		raw, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		cfg := p.base.Clone()
		cfg.ServerName = host
		cfg.InsecureSkipVerify = !p.verifies(host)

		conn := tls.Client(raw, cfg)
		if err := conn.HandshakeContext(ctx); err != nil {
			_ = raw.Close() // the handshake error is the one worth reporting
			return nil, err
		}

		return conn, nil
	}

	return p
}

// SetCertificateValidation implements the application's CertificatePolicy port.
func (p *Policy) SetCertificateValidation(m auth.CertificateValidation) { p.mode.Store(m) }

func (p *Policy) verifies(host string) bool {
	switch p.mode.Load().(auth.CertificateValidation) {
	case auth.CertsDisabled:
		return false
	case auth.CertsLocalDisabled:
		return !IsLocal(host)
	}

	return true
}

// IsLocal reports whether host is on this machine or the local network: a loopback, private or
// link-local address, "localhost", a single-label name, or a name under .local, .lan, .home.arpa
// or .localhost.
func IsLocal(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if a, err := netip.ParseAddr(host); err == nil {
		a = a.Unmap()
		return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
	}

	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}

	for _, suffix := range []string{".local", ".lan", ".home.arpa", ".localhost", ".internal"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	return false
}
