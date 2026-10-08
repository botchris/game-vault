package rpc_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"connectrpc.com/connect"

	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// hostTransport sends requests with another Host header and extra headers: a site's own name
// pointing at this machine (DNS rebinding), or a request through a proxy.
type hostTransport struct {
	host    string
	headers map[string]string
}

func (t hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.host != "" {
		r.Host = t.host
	}
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func clientWith(host string, headers map[string]string) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: hostTransport{host: host, headers: headers}, Jar: jar}
}

func TestAccessControl(t *testing.T) {
	ctx := context.Background()
	c := newServer(t, &fakeProvider{})
	listGames := func(hc *http.Client) error {
		_, err := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
		return err
	}
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)

	// Defaults: this computer is a trusted network, so it gets in without signing in and may
	// create the user.
	st, err := local.GetAuthStatus(ctx, connect.NewRequest(&pb.GetAuthStatusRequest{}))
	if err != nil || st.Msg.Principal.GetMethod() != "trusted" || !st.Msg.CanSetup || !st.Msg.Trusted {
		t.Fatalf("local status: %+v %v", st, err)
	}

	// Same machine, but a site's own name (DNS rebinding) or a proxy: not trusted.
	rebinding := clientWith("evil.example:8080", nil)
	proxied := clientWith("", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	for name, hc := range map[string]*http.Client{"rebinding": rebinding, "proxied": proxied} {
		if err := listGames(hc); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("%s request must be rejected, got %v", name, err)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/media/covers/x", nil)
	if res, err := rebinding.Do(req); err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("media through rebinding must be refused: %v %v", res, err)
	}
	rebindingAuth := gamevaultv1connect.NewAuthServiceClient(rebinding, c.baseURL)
	if _, err := rebindingAuth.Setup(ctx, connect.NewRequest(&pb.SetupRequest{Username: "admin", Password: "correct horse"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("setup must only work from a trusted network, got %v", err)
	}

	// Requiring authentication before a user exists would lock the owner out.
	settings, err := local.GetAuthSettings(ctx, connect.NewRequest(&pb.GetAuthSettingsRequest{}))
	if err != nil || settings.Msg.HasUser || settings.Msg.ClientAddress != "127.0.0.1" {
		t.Fatalf("settings: %+v %v", settings, err)
	}
	s := settings.Msg.Settings
	s.Authentication = "required"
	if _, err := local.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("lockout must be refused, got %v", err)
	}

	// Create the user; signing in then works from anywhere.
	if _, err := local.Setup(ctx, connect.NewRequest(&pb.SetupRequest{Username: "admin", Password: "correct horse"})); err != nil {
		t.Fatal(err)
	}
	if _, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "wrong password"})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("bad password: got %v", err)
	}
	login, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "correct horse"}))
	if err != nil || login.Msg.Principal.Method != "session" || !strings.Contains(login.Header().Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("login: %+v %v", login, err)
	}
	if err := listGames(rebinding); err != nil {
		t.Fatalf("the session cookie must authenticate: %v", err)
	}

	// Authentication required: even this computer must sign in.
	s.Authentication = "required"
	if _, err := local.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s})); err != nil {
		t.Fatal(err)
	}
	if err := listGames(http.DefaultClient); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("with authentication required, the computer must sign in, got %v", err)
	}

	// Back to trusted networks, but only another network is trusted: this computer is not.
	s.Authentication, s.TrustedNetworks = "trusted_networks", []string{"10.0.0.0/8"}
	settingsClient := gamevaultv1connect.NewAuthServiceClient(rebinding, c.baseURL) // signed in
	updated, err := settingsClient.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s}))
	if err != nil || updated.Msg.Settings.TrustedNetworks[0] != "10.0.0.0/8" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if err := listGames(http.DefaultClient); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("an untrusted network must sign in, got %v", err)
	}
	if _, err := settingsClient.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: &pb.AuthSettings{
		Authentication: "trusted_networks", TrustedNetworks: []string{"not a network"}}})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("invalid networks must be refused, got %v", err)
	}

	// Trusted again: the password can be reset without the current one (forgotten password),
	// which signs out every session.
	s.TrustedNetworks = []string{"127.0.0.0/8"}
	if _, err := settingsClient.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s})); err != nil {
		t.Fatal(err)
	}
	if _, err := local.ChangePassword(ctx, connect.NewRequest(&pb.ChangePasswordRequest{NewPassword: "another horse"})); err != nil {
		t.Fatal(err)
	}
	if err := listGames(rebinding); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("old sessions must be closed after a password reset, got %v", err)
	}
	if _, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "another horse"})); err != nil {
		t.Fatalf("the new password must work: %v", err)
	}
}
