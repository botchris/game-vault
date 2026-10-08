package media

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"

	"gamevault/internal/domain/game"
)

// ErrNoAsset means the image is neither stored locally nor known to come from anywhere.
var ErrNoAsset = errors.New("asset not found")

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

// Asset returns one of the game's stored images. If the file is missing (never downloaded yet, or
// deleted), it is downloaded again from where it came from and stored, so browsing the catalogue
// only goes online once per image.
func (s *Service) Asset(ctx context.Context, id game.ID, name string) (Image, error) {
	if img, ok, err := s.store.GetAsset(id, name); err != nil || ok {
		return img, err
	}
	remote, ok := s.store.AssetSource(id, name)
	if !ok {
		return Image{}, ErrNoAsset
	}
	v, err, _ := s.group.Do("asset:"+string(id)+":"+name, func() (any, error) {
		return s.downloadAsset(context.WithoutCancel(ctx), id, name, remote)
	})
	if err != nil {
		return Image{}, err
	}
	return v.(Image), nil
}

func (s *Service) downloadAsset(ctx context.Context, id game.ID, name, remote string) (Image, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	img, err := s.fetch.Fetch(ctx, remote)
	if err != nil {
		return Image{}, fmt.Errorf("downloading %s: %w", name, err)
	}
	if err := s.store.PutAsset(GameRef{g.ID(), g.Title()}, name, img); err != nil {
		return Image{}, err
	}
	return img, nil
}

// registerAssets records where the sheet's images come from and downloads the missing ones in the
// background, so the next view of the sheet (or any view offline) is served from disk.
func (s *Service) registerAssets(ctx context.Context, g GameRef, d *GameDetails) {
	sources := assignAssets(d)
	if err := s.store.SetAssetSources(g, sources); err != nil {
		s.log.Warn("recording game assets", "game", g.Title, "error", err)
		return
	}
	var missing []string
	for name := range sources {
		if _, ok, _ := s.store.GetAsset(g.ID, name); !ok {
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
			if _, err := s.Asset(bg, g.ID, name); err != nil {
				s.log.Debug("prefetching game asset", "game", g.Title, "asset", name, "error", err)
				continue
			}
			n++
		}
		s.log.Debug("game assets downloaded", "game", g.Title, "count", n)
	}()
}
