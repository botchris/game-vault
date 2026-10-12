package rpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

const sessionCookie = "gv_session"

type ctxKey int

const (
	ctxRequest ctxKey = iota
	ctxPrincipal
)

func requestFrom(ctx context.Context) auth.Request {
	r, _ := ctx.Value(ctxRequest).(auth.Request)
	return r
}

func principalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(ctxPrincipal).(auth.Principal)
	return p, ok
}

// authRequest extracts what the auth use cases need from an HTTP request.
func authRequest(r *http.Request) auth.Request {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}

	ip, _ := netip.ParseAddr(peer)

	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.Trim(strings.ToLower(host), "[]")
	_, hostErr := netip.ParseAddr(host)

	req := auth.Request{
		ClientIP:      ip,
		HostIsAddress: hostErr == nil || host == "localhost",
		Forwarded: r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" ||
			r.Header.Get("X-Forwarded-Host") != "" || r.Header.Get("X-Real-IP") != "",
		HTTPS:     r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		UserAgent: r.UserAgent(),
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		req.SessionToken = c.Value
	}

	return req
}

// isProtected reports whether a path needs an authenticated caller: every API service except
// AuthService, and media. The static UI is public so the login page can load.
func isProtected(path string) bool {
	if strings.HasPrefix(path, "/gamevault.v1.AuthService/") {
		return false
	}

	return strings.HasPrefix(path, "/gamevault.v1.") || strings.HasPrefix(path, "/media/")
}

// tokenProcedures are the only paths an access token may call: it looks up what the user owns
// and nothing else.
var tokenProcedures = []string{gamevaultv1connect.OwnershipServiceCheckOwnershipProcedure}

// accessToken returns the Game Vault access token of an "Authorization: Bearer gvt_…" header.
// Other credentials (a proxy's Basic or Bearer header) are not Game Vault's and are ignored.
func accessToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	token = strings.TrimSpace(token)

	return token, ok && strings.EqualFold(scheme, "Bearer") && auth.IsAccessToken(token)
}

// serveWithToken lets an access token call the ownership lookup and nothing else. The token alone
// identifies the caller: neither a session cookie nor a trusted network adds to it, and the
// proxy and host name rules are unaffected (a token never makes a request trusted).
func serveWithToken(svc *appauth.Service, next http.Handler, w http.ResponseWriter, r *http.Request, token string) {
	// The escaped path is compared, so an encoded "/" ("%2F") is not mistaken for the procedure.
	path := r.URL.EscapedPath()
	if !slices.Contains(tokenProcedures, path) {
		denyUnauthenticated(w, path)
		return
	}

	req := authRequest(r)
	req.SessionToken = ""
	ctx := context.WithValue(r.Context(), ctxRequest, req)

	p, err := svc.AuthenticateToken(ctx, req, token)
	switch {
	case err == nil:
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxPrincipal, p)))
	case errors.Is(err, auth.ErrTooManyAttempts):
		denyThrottled(w)
	case errors.Is(err, auth.ErrUnauthenticated):
		denyUnauthenticated(w, path)
	default:
		http.Error(w, "authentication failed", http.StatusInternalServerError)
	}
}

// denyThrottled answers a throttled request. Retry-After is the whole window, the longest the
// caller may have to wait.
func denyThrottled(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(int(appauth.ThrottleWindow()/time.Second)))
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"code":"resource_exhausted","message":"too many failed attempts; wait a few minutes"}`))
}

// authMiddleware identifies the caller of API requests and rejects unauthenticated ones.
func authMiddleware(svc *appauth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := accessToken(r); ok {
			serveWithToken(svc, next, w, r, token)
			return
		}

		path := r.URL.Path
		if !isProtected(path) && !strings.HasPrefix(path, "/gamevault.v1.") {
			next.ServeHTTP(w, r) // static UI: nothing to identify
			return
		}

		req := authRequest(r)
		ctx := context.WithValue(r.Context(), ctxRequest, req)

		p, err := svc.Authenticate(ctx, req)
		switch {
		case err == nil:
			ctx = context.WithValue(ctx, ctxPrincipal, p)
		case isProtected(path):
			if !errors.Is(err, auth.ErrUnauthenticated) {
				http.Error(w, "authentication failed", http.StatusInternalServerError)
				return
			}

			denyUnauthenticated(w, path)

			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func denyUnauthenticated(w http.ResponseWriter, path string) {
	if strings.HasPrefix(path, "/gamevault.v1.") {
		// Connect protocol error, so clients see code "unauthenticated".
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":"unauthenticated","message":"authentication required"}`))

		return
	}

	http.Error(w, "authentication required", http.StatusUnauthorized)
}

// sessionCookieFor builds the session cookie. HttpOnly keeps it away from scripts; SameSite=Lax
// plus Connect's JSON-only requests block cross-site request forgery.
func sessionCookieFor(token string, secure bool, maxAge time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge / time.Second),
	}
	if maxAge <= 0 {
		c.MaxAge = -1
	}

	return c
}
