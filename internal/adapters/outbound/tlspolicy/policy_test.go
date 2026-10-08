package tlspolicy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gamevault/internal/domain/auth"
)

func TestPolicy(t *testing.T) {
	// A server with a self-signed certificate on 127.0.0.1, a local address.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	p := Install(transport)
	client := &http.Client{Transport: transport}
	get := func() error {
		res, err := client.Get(srv.URL)
		if err == nil {
			res.Body.Close()
		}
		return err
	}

	if err := get(); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("certificates must be checked by default, got %v", err)
	}
	p.SetCertificateValidation(auth.CertsLocalDisabled)
	transport.CloseIdleConnections()
	if err := get(); err != nil {
		t.Fatalf("local addresses skip the check: %v", err)
	}
	p.SetCertificateValidation(auth.CertsDisabled)
	transport.CloseIdleConnections()
	if err := get(); err != nil {
		t.Fatalf("disabled: %v", err)
	}
	p.SetCertificateValidation(auth.CertsEnabled)
	transport.CloseIdleConnections()
	if err := get(); err == nil {
		t.Fatal("enabled again: the self-signed certificate must be refused")
	}
}

func TestIsLocal(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "192.168.1.10": true, "10.0.0.2": true, "[::1]": true, "fe80::1": true,
		"localhost": true, "nas": true, "nas.local": true, "router.lan": true, "box.home.arpa": true,
		"store.steampowered.com": false, "8.8.8.8": false, "api.gog.com": false,
	} {
		if got := IsLocal(host); got != want {
			t.Errorf("IsLocal(%q) = %v, want %v", host, got, want)
		}
	}
}
