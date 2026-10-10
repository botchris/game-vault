package media

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// detailsTTL is how long cached details are used before asking the providers again.
const detailsTTL = 30 * 24 * time.Hour

// Video is a trailer: an HLS stream (Steam) or a YouTube video (TheGamesDB).
type Video struct {
	Title     string
	Thumbnail string
	HLSURL    string
	YouTubeID string

	// ThumbAsset is the local name of the downloaded thumbnail (see Service.Asset).
	ThumbAsset string
}

// Screenshot is an in-game image. The *URL fields are the remote originals; the *Asset fields
// name the local copies in the game's folder.
type Screenshot struct {
	ThumbURL   string
	FullURL    string
	ThumbAsset string
	FullAsset  string
}

// GameDetails is the descriptive sheet of a game, as gathered from the metadata providers.
type GameDetails struct {
	Summary       string // plain text; paragraphs separated by blank lines
	Genres        []string
	Developers    []string
	Publishers    []string
	ReleaseDate   string // YYYY-MM-DD when known, otherwise as the provider wrote it
	AgeRating     string // e.g. "PEGI 18", "M - Mature"
	Players       string // e.g. "1-4", "4 (co-op)"
	Metacritic    int    // 0 = unknown
	MetacriticURL string
	Website       string
	StoreURL      string
	Videos        []Video
	Screenshots   []Screenshot

	// Sources lists the providers that contributed, in chain order.
	Sources   []provider.ID
	Language  string
	FetchedAt time.Time
}

// empty reports whether nothing useful was found.
func (d GameDetails) empty() bool {
	return d.Summary == "" && len(d.Genres) == 0 && len(d.Developers) == 0 && len(d.Publishers) == 0 &&
		d.ReleaseDate == "" && len(d.Videos) == 0 && len(d.Screenshots) == 0
}

// fill copies into d the fields it lacks from o (Plex-style: the first agent wins, later ones fill gaps).
func (d *GameDetails) fill(o GameDetails) bool {
	used := false
	str := func(dst *string, v string) {
		if *dst == "" && v != "" {
			*dst, used = v, true
		}
	}
	list := func(dst *[]string, v []string) {
		if len(*dst) == 0 && len(v) > 0 {
			*dst, used = v, true
		}
	}

	str(&d.Summary, o.Summary)
	list(&d.Genres, o.Genres)
	list(&d.Developers, o.Developers)
	list(&d.Publishers, o.Publishers)
	str(&d.ReleaseDate, o.ReleaseDate)
	str(&d.AgeRating, o.AgeRating)
	str(&d.Players, o.Players)
	str(&d.Website, o.Website)
	str(&d.StoreURL, o.StoreURL)

	if d.Metacritic == 0 && o.Metacritic != 0 {
		d.Metacritic, d.MetacriticURL, used = o.Metacritic, o.MetacriticURL, true
	}

	if len(o.Videos) > 0 { // trailers from every source are useful: Steam HLS + TheGamesDB YouTube
		d.Videos, used = append(d.Videos, o.Videos...), true
	}

	if len(d.Screenshots) == 0 && len(o.Screenshots) > 0 {
		d.Screenshots, used = o.Screenshots, true
	}

	return used
}

// MetadataProvider is the port each details source implements (Steam store, TheGamesDB...).
type MetadataProvider interface {
	Provider

	// Applies reports, without any network call, whether the provider can know this game.
	Applies(q CoverQuery) bool

	// Details returns what the provider knows in the given UI language ("en", "es"...);
	// nil when it does not know the game.
	Details(ctx context.Context, q CoverQuery, language string, settings schema.Settings) (*GameDetails, error)
}

// DetailsStore is the port that caches details per game and language.
type DetailsStore interface {
	// Get returns the cached details of a game in a language, and whether there are any.
	Get(ctx context.Context, id game.ID, language string) (GameDetails, bool, error)

	// Put caches the details of a game, replacing those in the same language.
	Put(ctx context.Context, id game.ID, d GameDetails) error

	// Delete forgets every cached sheet of a game, in all languages.
	Delete(ctx context.Context, id game.ID) error

	// Summaries returns the catalog-level facts (genres, release date) of every cached sheet
	// in a language, with the time each sheet was fetched.
	Summaries(ctx context.Context, language string) (map[game.ID]Summary, error)
}

// Summary is what the catalog needs from a game's details: enough to filter and sort.
type Summary struct {
	Genres      []string
	ReleaseDate string
	FetchedAt   time.Time
}

var reYear = regexp.MustCompile(`\b(19[5-9]\d|20\d\d)\b`)

// Year extracts the release year from any date format the providers use ("2010-05-18",
// "14 MAY 2019", "18 ABR 2011"); 0 when unknown.
func (s Summary) Year() int {
	if m := reYear.FindString(s.ReleaseDate); m != "" {
		y, _ := strconv.Atoi(m)
		return y
	}

	return 0
}

// GameDetails returns the game's details in the given language: from the cache when fresh,
// otherwise merged from every enabled, applicable metadata provider. refresh skips the cache.
func (s *Service) GameDetails(ctx context.Context, id game.ID, language string, refresh bool) (GameDetails, []string, error) {
	if language == "" {
		language = "en"
	}

	g, err := s.games.Get(ctx, id)
	if err != nil {
		return GameDetails{}, nil, err
	}

	ref := GameRef{g.ID(), g.Title()}

	if !refresh {
		if d, ok, err := s.details.Get(ctx, id, language); err != nil {
			return d, nil, err
		} else if ok && s.now().Sub(d.FetchedAt) < detailsTTL {
			s.registerAssets(ctx, ref, &d)
			return d, nil, nil
		}
	}

	type out struct {
		d        GameDetails
		warnings []string
	}

	v, err, _ := s.group.Do("details:"+string(id)+":"+language, func() (any, error) {
		d, w, err := s.fetchDetails(context.WithoutCancel(ctx), id, language)
		return out{d, w}, err
	})
	if err != nil {
		return GameDetails{}, nil, err
	}

	r := v.(out)
	s.registerAssets(ctx, ref, &r.d)

	return r.d, r.warnings, nil
}

func (s *Service) fetchDetails(ctx context.Context, id game.ID, language string) (GameDetails, []string, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return GameDetails{}, nil, err
	}

	chain, err := s.enabled(ctx, provider.KindMetadata)
	if err != nil {
		return GameDetails{}, nil, err
	}

	q := QueryFor(g, "") // a game's sheet describes it on every system
	merged := GameDetails{
		Language:  language,
		FetchedAt: s.now(),
	}

	var warnings []string

	for _, p := range chain {
		impl := s.metadata[p.ID()]
		if !impl.Applies(q) {
			continue
		}

		d, err := impl.Details(ctx, q, language, p.Settings())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			s.log.Warn("metadata provider failed", "provider", p.ID(), "game", g.Title(), "error", err)

			continue
		}

		if d != nil && merged.fill(*d) {
			merged.Sources = append(merged.Sources, p.ID())
		}
	}
	// Cache even an empty sheet so unknown games are not looked up on every view; failures are not cached.
	if len(warnings) == 0 || !merged.empty() {
		if err := s.details.Put(ctx, id, merged); err != nil {
			s.log.Warn("caching details", "error", err)
		}
	}

	return merged, warnings, nil
}
