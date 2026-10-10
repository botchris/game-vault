// Package gamedata stores every downloaded image of a game in its own folder (Plex-style) and
// implements media.AssetStore:
//
//	config/game-data/
//	  Red Dead Redemption [019b11c2-…]/
//	    cover-ps3-1a2b3c4d.jpg   the cover of each edition, by system (cover-….missing when the
//	                             last lookup found nothing); cover.jpg is a cover from before editions
//	    assets.json          where each sheet image came from, to download it again if missing
//	    screenshot-3f9a….jpg full-size screenshots
//	    thumb-77b2….jpg      screenshot thumbnails
//	    trailer-a1c0….jpg    trailer posters
//
// Folders are named after the game and its id; the id makes them unique and lets a folder be found
// even after the game is renamed (the folder is then renamed to match).
package gamedata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

const (
	legacyCover   = "cover"
	legacyMissing = "cover.missing"
	sourcesFile   = "assets.json"
	maxTitleLen   = 80
)

var (
	extByType = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}
	typeByExt = map[string]string{".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".gif": "image/gif"}

	reID       = regexp.MustCompile(`^[0-9a-fA-F-]{1,64}$`)
	reName     = regexp.MustCompile(`^[a-z0-9-]{1,80}$`)
	reDirID    = regexp.MustCompile(`\[([0-9a-fA-F-]{1,64})\]$`)
	reBadChars = regexp.MustCompile(`[/\\:*?"<>|]+`)
	reSpaces   = regexp.MustCompile(`\s+`)
	reNotSlug  = regexp.MustCompile(`[^a-z0-9]+`)

	errBadID   = errors.New("invalid game id")
	errBadName = errors.New("invalid asset name")
)

// Store implements media.AssetStore.
type Store struct {
	root string

	mu      sync.Mutex
	dirs    map[game.ID]string            // id → folder name
	sources map[game.ID]map[string]string // loaded assets.json, by game
}

var _ media.AssetStore = (*Store)(nil)

// Open indexes the existing game folders under root (creating it if needed).
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	s := &Store{
		root:    root,
		dirs:    map[game.ID]string{},
		sources: map[game.ID]map[string]string{},
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if m := reDirID.FindStringSubmatch(e.Name()); e.IsDir() && m != nil {
			s.dirs[game.ID(m[1])] = e.Name()
		}
	}

	return s, nil
}

// folderName is "<title> [<id>]" with characters that are invalid in file names removed.
func folderName(g media.GameRef) string {
	t := reBadChars.ReplaceAllString(g.Title, " ")
	t = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, t)

	t = strings.Trim(reSpaces.ReplaceAllString(t, " "), " .")
	if r := []rune(t); len(r) > maxTitleLen {
		t = strings.TrimSpace(string(r[:maxTitleLen]))
	}

	if t == "" {
		t = "Game"
	}

	return t + " [" + string(g.ID) + "]"
}

// dir returns the folder of a game if it exists.
func (s *Store) dir(id game.ID) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.dirs[id]

	return filepath.Join(s.root, d), ok
}

// ensureDir returns the game's folder, creating it or renaming it after a title change.
func (s *Store) ensureDir(g media.GameRef) (string, error) {
	if !reID.MatchString(string(g.ID)) {
		return "", errBadID
	}

	want := folderName(g)

	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, ok := s.dirs[g.ID]; ok {
		if cur != want {
			if err := os.Rename(filepath.Join(s.root, cur), filepath.Join(s.root, want)); err == nil {
				s.dirs[g.ID] = want
			} // a failed rename keeps the old name: still correct, only less readable
		}

		return filepath.Join(s.root, s.dirs[g.ID]), nil
	}

	if err := os.MkdirAll(filepath.Join(s.root, want), 0o755); err != nil {
		return "", err
	}

	s.dirs[g.ID] = want

	return filepath.Join(s.root, want), nil
}

func readImage(dir, name string) (media.Image, bool, error) {
	for ext, ct := range typeByExt {
		data, err := os.ReadFile(filepath.Join(dir, name+ext))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return media.Image{}, false, err
		}

		return media.Image{
			Data:        data,
			ContentType: ct,
		}, true, nil
	}

	return media.Image{}, false, nil
}

func removeImage(dir, name string) error {
	for ext := range typeByExt {
		if err := os.Remove(filepath.Join(dir, name+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
}

// writeFile writes atomically, so readers never see a half-written file.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func writeImage(dir, name string, img media.Image) error {
	ext, ok := extByType[strings.ToLower(img.ContentType)]
	if !ok {
		ext = ".jpg"
	}

	if err := removeImage(dir, name); err != nil { // the type may have changed
		return err
	}

	return writeFile(filepath.Join(dir, name+ext), img.Data)
}

// Cover ------------------------------------------------------------------------------------------

// coverFile is the file name, without extension, of an edition's cover: "cover-xbox-360-1a2b3c4d".
// The slug keeps folders readable; the hash keeps apart systems with the same slug ("PS4/Pro",
// "PS4 Pro") and makes any system name safe as a file name. A game without copies uses
// "cover-game".
func coverFile(system string) string {
	if system == "" {
		return legacyCover + "-game"
	}

	slug := strings.Trim(reNotSlug.ReplaceAllString(strings.ToLower(system), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}

	if slug == "" {
		slug = "system"
	}

	sum := sha256.Sum256([]byte(system))

	return legacyCover + "-" + slug + "-" + hex.EncodeToString(sum[:4])
}

// isCoverName reports whether a file name (without extension) belongs to a cover, so sheet images
// can never use or prune it.
func isCoverName(name string) bool {
	return name == legacyCover || strings.HasPrefix(name, legacyCover+"-")
}

// GetCover returns the stored cover of a game's edition, if any.
func (s *Store) GetCover(id game.ID, system string) (media.Image, bool, error) {
	dir, ok := s.dir(id)
	if !ok {
		return media.Image{}, false, nil
	}

	return readImage(dir, coverFile(system))
}

// PutCover stores the cover of a game's edition and clears its missing marker.
func (s *Store) PutCover(g media.GameRef, system string, img media.Image) error {
	dir, err := s.ensureDir(g)
	if err != nil {
		return err
	}

	_ = os.Remove(filepath.Join(dir, coverFile(system)+".missing")) // absent is fine

	return writeImage(dir, coverFile(system), img)
}

// MarkCoverMissing records when no provider had a cover for a game's edition.
func (s *Store) MarkCoverMissing(g media.GameRef, system string, at time.Time) error {
	dir, err := s.ensureDir(g)
	if err != nil {
		return err
	}

	return writeFile(filepath.Join(dir, coverFile(system)+".missing"), []byte(at.UTC().Format(time.RFC3339)))
}

// CoverMissingSince returns when a game's edition was marked as having no cover.
func (s *Store) CoverMissingSince(id game.ID, system string) (time.Time, bool) {
	dir, ok := s.dir(id)
	if !ok {
		return time.Time{}, false
	}

	b, err := os.ReadFile(filepath.Join(dir, coverFile(system)+".missing"))
	if err != nil {
		return time.Time{}, false
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))

	return t, err == nil
}

// ClearCoverMissing removes the missing marker of a game's edition, so its cover is looked up
// again; its stored cover, and the cover from before editions, stay.
func (s *Store) ClearCoverMissing(id game.ID, system string) error {
	dir, ok := s.dir(id)
	if !ok {
		return nil
	}

	if err := os.Remove(filepath.Join(dir, coverFile(system)+".missing")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

// DeleteCover removes the cover of a game's edition and its missing marker, and the cover from
// before editions, which the main edition would otherwise adopt instead of the new one.
func (s *Store) DeleteCover(id game.ID, system string) error {
	dir, ok := s.dir(id)
	if !ok {
		return nil
	}

	for _, name := range []string{coverFile(system) + ".missing", legacyMissing} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := removeImage(dir, legacyCover); err != nil {
		return err
	}

	return removeImage(dir, coverFile(system))
}

// DeleteCovers removes the covers and missing markers of every edition of a game.
func (s *Store) DeleteCovers(id game.ID) error {
	dir, ok := s.dir(id)
	if !ok {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if isCoverName(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}

	return nil
}

// AdoptLegacyCover makes the cover stored before editions (cover.jpg) the cover of the edition
// on system, and reports whether there was one. The main edition adopts it, so upgrading does not
// download every cover again.
func (s *Store) AdoptLegacyCover(g media.GameRef, system string) (media.Image, bool, error) {
	dir, ok := s.dir(g.ID)
	if !ok {
		return media.Image{}, false, nil
	}

	img, ok, err := readImage(dir, legacyCover)
	if err != nil || !ok {
		return media.Image{}, false, err
	}

	if err := s.PutCover(g, system, img); err != nil {
		return media.Image{}, false, err
	}

	_ = os.Remove(filepath.Join(dir, legacyMissing)) // absent is fine

	return img, true, removeImage(dir, legacyCover)
}

// Sheet images -----------------------------------------------------------------------------------

// GetAsset returns a stored sheet image (screenshot, artwork) of a game by name.
func (s *Store) GetAsset(id game.ID, name string) (media.Image, bool, error) {
	if !reName.MatchString(name) || isCoverName(name) {
		return media.Image{}, false, errBadName
	}

	dir, ok := s.dir(id)
	if !ok {
		return media.Image{}, false, nil
	}

	return readImage(dir, name)
}

// PutAsset stores a sheet image of a game under the given name.
func (s *Store) PutAsset(g media.GameRef, name string, img media.Image) error {
	if !reName.MatchString(name) || isCoverName(name) {
		return errBadName
	}

	dir, err := s.ensureDir(g)
	if err != nil {
		return err
	}

	return writeImage(dir, name, img)
}

func (s *Store) loadSources(id game.ID) map[string]string {
	s.mu.Lock()
	if m, ok := s.sources[id]; ok {
		s.mu.Unlock()
		return m
	}

	d, ok := s.dirs[id]
	s.mu.Unlock()

	m := map[string]string{}

	if ok {
		var file struct {
			Sources map[string]string `json:"sources"`
		}
		if b, err := os.ReadFile(filepath.Join(s.root, d, sourcesFile)); err == nil && json.Unmarshal(b, &file) == nil && file.Sources != nil {
			m = file.Sources
		}
	}

	s.mu.Lock()
	s.sources[id] = m
	s.mu.Unlock()

	return m
}

// SetAssetSources records the remote URL each stored sheet image came from.
func (s *Store) SetAssetSources(g media.GameRef, sources map[string]string) error {
	for name := range sources {
		if !reName.MatchString(name) || isCoverName(name) {
			return errBadName
		}
	}

	if maps.Equal(s.loadSources(g.ID), sources) {
		return nil // nothing changed: no disk write on every view
	}

	dir, err := s.ensureDir(g)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(struct {
		Title   string            `json:"title"`
		Sources map[string]string `json:"sources"`
	}{g.Title, sources}, "", "  ")
	if err != nil {
		return err
	}

	if err := writeFile(filepath.Join(dir, sourcesFile), b); err != nil {
		return err
	}

	s.mu.Lock()
	s.sources[g.ID] = maps.Clone(sources)
	s.mu.Unlock()
	// Delete images that are no longer part of the sheet.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if _, isImage := typeByExt[filepath.Ext(e.Name())]; isImage && !isCoverName(name) {
			if _, keep := sources[name]; !keep {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}

	return nil
}

// AssetSource returns the remote URL a stored sheet image came from.
func (s *Store) AssetSource(id game.ID, name string) (string, bool) {
	u, ok := s.loadSources(id)[name]
	return u, ok
}

// DeleteAll removes everything stored for a game: cover and sheet images.
func (s *Store) DeleteAll(id game.ID) error {
	dir, ok := s.dir(id)
	if !ok {
		return nil
	}

	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.dirs, id)
	delete(s.sources, id)
	s.mu.Unlock()

	return nil
}

// MigrateLegacyCovers moves covers from the old flat layout (config/covers/<id>.jpg and
// <id>.missing) into each game's folder, then removes the old directory if it ends up empty.
// Covers of games that no longer exist are deleted.
func (s *Store) MigrateLegacyCovers(legacyDir string, titles map[game.ID]string) (int, error) {
	entries, err := os.ReadDir(legacyDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	moved := 0

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		ext := filepath.Ext(e.Name())
		id := game.ID(strings.TrimSuffix(e.Name(), ext))
		src := filepath.Join(legacyDir, e.Name())
		title, known := titles[id]

		_, isImage := typeByExt[ext]
		if !known || (!isImage && ext != ".missing") {
			_ = os.Remove(src)
			continue
		}

		dir, err := s.ensureDir(media.GameRef{
			ID:    id,
			Title: title,
		})
		if err != nil {
			return moved, err
		}

		dst := filepath.Join(dir, legacyCover+ext)
		if ext == ".missing" {
			dst = filepath.Join(dir, legacyMissing)
		}

		if err := os.Rename(src, dst); err != nil {
			return moved, err
		}

		moved++
	}

	_ = os.Remove(legacyDir) // only succeeds when empty

	return moved, nil
}
