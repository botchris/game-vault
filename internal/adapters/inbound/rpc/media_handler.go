package rpc

import (
	"errors"
	"net/http"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// coverHandler serves GET /media/covers/{id} (the main edition's cover) and
// GET /media/covers/{id}/{system} (an edition's; the system is URL-escaped, so it may contain
// spaces or slashes). Images are plain HTTP rather than RPC so browsers can load them with
// <img src> and cache them. Clients add ?v=<updatedAt> to bust the cache.
func coverHandler(m *media.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := game.ID(r.PathValue("id"))

		var (
			img media.Image
			err error
		)

		if system := r.PathValue("system"); system != "" {
			img, err = m.EditionCover(r.Context(), id, system)
		} else {
			img, err = m.Cover(r.Context(), id)
		}

		switch {
		case errors.Is(err, media.ErrNoCover), errors.Is(err, game.ErrGameNotFound):
			w.Header().Set("Cache-Control", "no-store") // a cover may be found later: never cache the miss
			http.Error(w, "no cover", http.StatusNotFound)

			return
		case err != nil:
			http.Error(w, "cover unavailable", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", img.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=604800")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(img.Data)
	})
}

// imageProxyHandler serves GET /media/proxy?url=… for provider thumbnails (see media.ProxyImage).
func imageProxyHandler(m *media.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img, err := m.ProxyImage(r.Context(), r.URL.Query().Get("url"))
		switch {
		case errors.Is(err, media.ErrImageNotAllowed):
			http.Error(w, "image host not allowed", http.StatusForbidden)
			return
		case err != nil:
			http.Error(w, "image unavailable", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", img.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(img.Data)
	})
}

// gameAssetHandler serves GET /media/games/{id}/assets/{name}: an image of the game's sheet from its
// folder, downloading it first if it is not stored yet (see media.Service.Asset).
func gameAssetHandler(m *media.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img, err := m.Asset(r.Context(), game.ID(r.PathValue("id")), r.PathValue("name"))
		switch {
		case errors.Is(err, media.ErrNoAsset), errors.Is(err, game.ErrGameNotFound):
			http.Error(w, "no such asset", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, "asset unavailable", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", img.ContentType)
		// Names change when the image changes, so they can be cached for long.
		w.Header().Set("Cache-Control", "private, max-age=2592000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(img.Data)
	})
}

// gameAssetPath is the URL path the UI uses for a stored asset.
func gameAssetPath(id game.ID, name string) string {
	return "/media/games/" + string(id) + "/assets/" + name
}
