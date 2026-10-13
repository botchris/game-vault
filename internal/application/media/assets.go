package media

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
)

// ErrNoAsset means the image is neither stored locally nor known to come from anywhere.
var ErrNoAsset = errkind.New(errkind.NotFound, "asset not found")

// assetName derives a stable local name from the remote URL, so the same image keeps its file
// across refreshes and languages, and a changed image gets a new one.
func assetName(kind, remote string) string {
	sum := sha1.Sum([]byte(remote))
	return kind + "-" + hex.EncodeToString(sum[:6])
}

// assignAssets gives every remote image of the sheet a local name and returns name → remote URL.
func assignAssets(d *GameDetails) map[string]string {
	sources := map[string]string{}
	add := func(kind, remote string) string {
		if remote == "" {
			return ""
		}

		name := assetName(kind, remote)
		sources[name] = remote

		return name
	}

	for i := range d.Screenshots {
		s := &d.Screenshots[i]
		s.FullAsset = add("screenshot", s.FullURL)
		s.ThumbAsset = add("thumb", s.ThumbURL)
	}

	for i := range d.Videos {
		d.Videos[i].ThumbAsset = add("trailer", d.Videos[i].Thumbnail)
	}

	return sources
}

// Assets serves the images of the games' sheets (screenshots, trailer thumbnails) from the game's
// folder, downloading each one once from where it came from.
type Assets struct {
	games     game.Repository
	store     AssetStore
	downloads *Downloads
	log       *slog.Logger
	group     singleflight.Group
}

// NewAssets builds the sheet images service. Options: WithLogger.
func NewAssets(games game.Repository, store AssetStore, downloads *Downloads, opts ...Option) *Assets {
	o := newOptions(opts)

	return &Assets{
		games:     games,
		store:     store,
		downloads: downloads,
		log:       o.log,
	}
}

// Asset returns one of the game's stored images. If the file is missing (never downloaded yet, or
// deleted), it is downloaded again from where it came from and stored, so browsing the catalog
// only goes online once per image.
func (a *Assets) Asset(ctx context.Context, id game.ID, name string) (Image, error) {
	if img, ok, err := a.store.GetAsset(id, name); err != nil || ok {
		return img, err
	}

	remote, ok := a.store.AssetSource(id, name)
	if !ok {
		return Image{}, ErrNoAsset
	}

	v, err, _ := a.group.Do("asset:"+string(id)+":"+name, func() (any, error) {
		return a.download(context.WithoutCancel(ctx), id, name, remote)
	})
	if err != nil {
		return Image{}, err
	}

	return v.(Image), nil
}

func (a *Assets) download(ctx context.Context, id game.ID, name, remote string) (Image, error) {
	g, err := a.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}

	img, err := a.downloads.Fetch(ctx, remote)
	if err != nil {
		return Image{}, fmt.Errorf("downloading %s: %w", name, err)
	}

	if err := a.store.PutAsset(GameRef{g.ID(), g.Title()}, name, img); err != nil {
		return Image{}, err
	}

	return img, nil
}

// registerAssets records where the sheet's images come from and downloads the missing ones in the
// background, so the next view of the sheet (or any view offline) is served from disk.
func (a *Assets) register(ctx context.Context, g GameRef, d *GameDetails) {
	sources := assignAssets(d)
	if err := a.store.SetAssetSources(g, sources); err != nil {
		a.log.Warn("recording game assets", "game", g.Title, "error", err)
		return
	}

	var missing []string

	for name := range sources {
		if _, ok, _ := a.store.GetAsset(g.ID, name); !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return
	}

	bg := context.WithoutCancel(ctx)

	go func() {
		n := 0

		for _, name := range missing {
			if _, err := a.Asset(bg, g.ID, name); err != nil {
				a.log.Debug("prefetching game asset", "game", g.Title, "asset", name, "error", err)
				continue
			}

			n++
		}

		a.log.Debug("game assets downloaded", "game", g.Title, "count", n)
	}()
}
