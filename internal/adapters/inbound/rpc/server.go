// Package rpc is the driving adapter: it exposes the application use cases over ConnectRPC
// (Connect, gRPC and gRPC-Web protocols) and optionally serves the built web UI.
package rpc

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	connectcors "connectrpc.com/cors"
	"github.com/rs/cors"

	appauth "gamevault/internal/application/auth"
	"gamevault/internal/application/media"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// Handlers groups the service implementations to mount.
type Handlers struct {
	Games   *GameHandler
	Sources *SourceHandler
	System  *SystemHandler
	Logs    *LogHandler

	// MediaRPC serves the provider and cover services; Media serves the cover images.
	MediaRPC *MediaHandler
	Media    *media.Service
	Auth     *AuthHandler

	// AuthService identifies callers for every request (see authMiddleware).
	AuthService *appauth.Service
}

// Options configures the HTTP handler.
type Options struct {
	// UIDir is a directory with the built web app (web/dist). Empty disables serving it.
	UIDir string

	// CORSOrigins lists origins allowed to call the API from a browser, for a UI hosted elsewhere.
	CORSOrigins []string

	// Log receives RPC logs. Optional.
	Log *slog.Logger
}

// NewHTTPHandler mounts the Connect services and, if configured, the web UI.
func NewHTTPHandler(h Handlers, opts Options) http.Handler {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	ic := connect.WithInterceptors(loggingInterceptor(log))
	mux := http.NewServeMux()
	mux.Handle(gamevaultv1connect.NewGameServiceHandler(h.Games, ic))
	mux.Handle(gamevaultv1connect.NewSourceServiceHandler(h.Sources, ic))
	mux.Handle(gamevaultv1connect.NewSystemServiceHandler(h.System, ic))
	mux.Handle(gamevaultv1connect.NewLogServiceHandler(h.Logs, ic))
	mux.Handle(gamevaultv1connect.NewProviderServiceHandler(h.MediaRPC, ic))
	mux.Handle(gamevaultv1connect.NewCoverServiceHandler(h.MediaRPC, ic))
	mux.Handle(gamevaultv1connect.NewLookupServiceHandler(h.MediaRPC, ic))
	mux.Handle(gamevaultv1connect.NewMetadataServiceHandler(h.MediaRPC, ic))
	mux.Handle(gamevaultv1connect.NewAuthServiceHandler(h.Auth, ic))
	mux.Handle("GET /media/covers/{id}", coverHandler(h.Media))
	mux.Handle("GET /media/proxy", imageProxyHandler(h.Media))
	mux.Handle("GET /media/games/{id}/assets/{name}", gameAssetHandler(h.Media))

	if opts.UIDir != "" {
		mux.Handle("/", spa(opts.UIDir))
	}

	handler := authMiddleware(h.AuthService, mux)
	if len(opts.CORSOrigins) > 0 {
		handler = cors.New(cors.Options{
			AllowedOrigins: opts.CORSOrigins,
			AllowedMethods: connectcors.AllowedMethods(),
			AllowedHeaders: connectcors.AllowedHeaders(),
			ExposedHeaders: connectcors.ExposedHeaders(),
		}).Handler(handler)
	}

	return handler
}

// spa serves static files from dir and falls back to index.html for client-side routes.
// index.html is never cached so a new build is picked up on reload; Vite's assets are
// content-hashed, so they can be cached forever.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))

			return
		}

		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		files.ServeHTTP(w, r)
	})
}
