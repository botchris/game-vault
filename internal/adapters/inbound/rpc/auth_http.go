package rpc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
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

// authMiddleware identifies the caller of API requests and rejects unauthenticated ones.
func authMiddleware(svc *appauth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(maxAge / time.Second),
	}
	if maxAge <= 0 {
		c.MaxAge = -1
	}

	return c
}
