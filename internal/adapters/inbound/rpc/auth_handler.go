package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// AuthHandler implements gamevaultv1connect.AuthServiceHandler. The caller's request info and
// principal come from authMiddleware through the context.
type AuthHandler struct {
	auth *appauth.Service
}

var _ gamevaultv1connect.AuthServiceHandler = (*AuthHandler)(nil)

// NewAuthHandler returns the AuthService handler backed by the auth service.
func NewAuthHandler(a *appauth.Service) *AuthHandler { return &AuthHandler{auth: a} }

func principalToPB(p *auth.Principal) *pb.Principal {
	if p == nil {
		return nil
	}

	return &pb.Principal{
		Method: string(p.Method),
		Name:   p.Name,
	}
}

// requirePrincipal returns the caller, refusing access tokens: a token is never enough for the
// AuthService (defense in depth; the middleware already refuses it).
func requirePrincipal(ctx context.Context) (auth.Principal, error) {
	p, ok := principalFrom(ctx)
	if !ok || p.Method == auth.MethodToken {
		return p, connect.NewError(connect.CodeUnauthenticated, auth.ErrUnauthenticated)
	}

	return p, nil
}

// GetAuthStatus reports whether a password is set and whether the caller is signed in or trusted.
func (h *AuthHandler) GetAuthStatus(ctx context.Context, _ *connect.Request[pb.GetAuthStatusRequest]) (*connect.Response[pb.GetAuthStatusResponse], error) {
	st, err := h.auth.Status(ctx, requestFrom(ctx))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.GetAuthStatusResponse{
		Principal:     principalToPB(st.Principal),
		SetupRequired: st.SetupRequired,
		CanSetup:      st.CanSetup,
		Trusted:       st.Trusted,
	}), nil
}

func (h *AuthHandler) sessionResponse(ctx context.Context, token string) *connect.Response[pb.LoginResponse] {
	res := connect.NewResponse(&pb.LoginResponse{})
	res.Header().Add("Set-Cookie", sessionCookieFor(token, requestFrom(ctx).HTTPS, appauth.SessionTTL()).String())

	return res
}

// Setup sets the first password and signs the caller in.
func (h *AuthHandler) Setup(ctx context.Context, req *connect.Request[pb.SetupRequest]) (*connect.Response[pb.SetupResponse], error) {
	token, p, err := h.auth.Setup(ctx, requestFrom(ctx), req.Msg.Username, req.Msg.Password)
	if err != nil {
		return nil, toConnectError(err)
	}

	res := connect.NewResponse(&pb.SetupResponse{Principal: principalToPB(&p)})
	res.Header().Add("Set-Cookie", sessionCookieFor(token, requestFrom(ctx).HTTPS, appauth.SessionTTL()).String())

	return res, nil
}

// Login checks the password and starts a session for the caller.
func (h *AuthHandler) Login(ctx context.Context, req *connect.Request[pb.LoginRequest]) (*connect.Response[pb.LoginResponse], error) {
	token, p, err := h.auth.Login(ctx, requestFrom(ctx), req.Msg.Username, req.Msg.Password)
	if err != nil {
		return nil, toConnectError(err)
	}

	res := h.sessionResponse(ctx, token)
	res.Msg.Principal = principalToPB(&p)

	return res, nil
}

// Logout ends the caller's session.
func (h *AuthHandler) Logout(ctx context.Context, _ *connect.Request[pb.LogoutRequest]) (*connect.Response[pb.LogoutResponse], error) {
	if err := h.auth.Logout(ctx, requestFrom(ctx).SessionToken); err != nil {
		return nil, toConnectError(err)
	}

	res := connect.NewResponse(&pb.LogoutResponse{})
	res.Header().Add("Set-Cookie", sessionCookieFor("", requestFrom(ctx).HTTPS, 0).String())

	return res, nil
}

// ChangePassword replaces the password after checking the current one.
func (h *AuthHandler) ChangePassword(ctx context.Context, req *connect.Request[pb.ChangePasswordRequest]) (*connect.Response[pb.ChangePasswordResponse], error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.auth.ChangePassword(ctx, p, req.Msg.Username, req.Msg.CurrentPassword, req.Msg.NewPassword); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.ChangePasswordResponse{}), nil
}

func authSettingsToPB(s auth.Settings) *pb.AuthSettings {
	return &pb.AuthSettings{
		Authentication:        string(s.Authentication),
		TrustedNetworks:       s.TrustedNetworks,
		CertificateValidation: string(s.CertificateValidation),
	}
}

// GetAuthSettings returns the access settings, such as the trusted networks.
func (h *AuthHandler) GetAuthSettings(ctx context.Context, _ *connect.Request[pb.GetAuthSettingsRequest]) (*connect.Response[pb.GetAuthSettingsResponse], error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}

	s, err := h.auth.Settings(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	r := requestFrom(ctx)

	out := &pb.GetAuthSettingsResponse{
		Settings:                authSettingsToPB(s),
		ClientAddress:           r.ClientIP.Unmap().String(),
		ClientInTrustedNetworks: s.Contains(r.ClientIP),
	}
	if u, err := h.auth.User(ctx); err == nil {
		out.HasUser, out.Username = true, u.Username
	} else if !errors.Is(err, auth.ErrNotFound) {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(out), nil
}

// UpdateAuthSettings saves the access settings.
func (h *AuthHandler) UpdateAuthSettings(ctx context.Context, req *connect.Request[pb.UpdateAuthSettingsRequest]) (*connect.Response[pb.UpdateAuthSettingsResponse], error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}

	in := req.Msg.Settings
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("settings are required"))
	}

	s, err := h.auth.UpdateSettings(ctx, auth.Settings{
		Authentication:        auth.Authentication(in.Authentication),
		TrustedNetworks:       in.TrustedNetworks,
		CertificateValidation: auth.CertificateValidation(in.CertificateValidation),
	})
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.UpdateAuthSettingsResponse{Settings: authSettingsToPB(s)}), nil
}

func accessTokenToPB(t auth.AccessToken) *pb.AccessToken {
	return &pb.AccessToken{
		Id:         t.ID,
		Name:       t.Name,
		CreatedAt:  ts(t.CreatedAt),
		LastUsedAt: optionalTS(t.LastUsedAt),
	}
}

// ListAccessTokens lists the access tokens, without their secrets.
func (h *AuthHandler) ListAccessTokens(ctx context.Context, _ *connect.Request[pb.ListAccessTokensRequest]) (*connect.Response[pb.ListAccessTokensResponse], error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}

	tokens, err := h.auth.AccessTokens(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.ListAccessTokensResponse{Tokens: make([]*pb.AccessToken, 0, len(tokens))}

	for _, t := range tokens {
		out.Tokens = append(out.Tokens, accessTokenToPB(t))
	}

	return connect.NewResponse(out), nil
}

// CreateAccessToken creates an access token and returns its secret, this once.
func (h *AuthHandler) CreateAccessToken(ctx context.Context, req *connect.Request[pb.CreateAccessTokenRequest]) (*connect.Response[pb.CreateAccessTokenResponse], error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}

	t, secret, err := h.auth.CreateAccessToken(ctx, req.Msg.Name)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.CreateAccessTokenResponse{
		Token:  accessTokenToPB(t),
		Secret: secret,
	}), nil
}

// RevokeAccessToken deletes an access token: programs using it are refused from their next request.
func (h *AuthHandler) RevokeAccessToken(ctx context.Context, req *connect.Request[pb.RevokeAccessTokenRequest]) (*connect.Response[pb.RevokeAccessTokenResponse], error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}

	if err := h.auth.RevokeAccessToken(ctx, req.Msg.Id); err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.RevokeAccessTokenResponse{}), nil
}
