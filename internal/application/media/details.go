package media

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// detailsTTL is how long cached details are used before asking the providers again.
const detailsTTL = 30 * 24 * time.Hour

// detailsLogicVersion is the version of the way sheets are gathered, stored in every sheet cached
// (GameDetails.LogicVersion). A sparse sheet cached by an older version (0: before the fallback pass)
// was built without the improvement, so it is fetched again once when opened instead of waiting for
// detailsTTL; a server still running the old code may cache such sheets after the upgrade's date, so
// a version is used rather than a time. Increment it when sheet gathering improves.
const detailsLogicVersion = 3 // 1: the fallback pass; 2: Wikipedia and IGDB; 3: searches by the game's name

// Video is a trailer: an HLS stream (Steam) or a YouTube video (IGDB, TheGamesDB).
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

// Attribution credits the source of a text whose license asks for it (Wikipedia's CC BY-SA: name the
// source, link to it and name the license). The zero value means no credit is needed.
type Attribution struct {
	// Name is the source as the UI names it ("Wikipedia").
	Name string

	// URL is the page the text comes from.
	URL string

	// License names the text's license ("CC BY-SA 4.0").
	License string

	// LicenseURL is the license's text (https://creativecommons.org/licenses/by-sa/4.0/).
	LicenseURL string
}

// GameDetails is the descriptive sheet of a game, as gathered from the metadata providers.
type GameDetails struct {
	Summary       string // plain text; paragraphs separated by blank lines
	SummarySource Attribution
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
	Sources []provider.ID

	// NotFoundBy lists the providers that were asked and know no such game (no match for the
	// title), so the sheet can say they were tried. It is not a failure: the sheet stays cacheable.
	NotFoundBy []provider.ID

	// FallbackSkipped reports that the sheet is sparse and its fallback pass did not complete: it was
	// not run (the background scan does not spend quota-limited providers' requests, a provider failed
	// in the first pass) or a provider failed in it. Opening the sheet runs the passes again.
	FallbackSkipped bool

	// LogicVersion is the detailsLogicVersion that gathered the sheet.
	LogicVersion int

	Language  string
	FetchedAt time.Time
}

// empty reports whether nothing useful was found.
func (d GameDetails) empty() bool {
	return d.Summary == "" && len(d.Genres) == 0 && len(d.Developers) == 0 && len(d.Publishers) == 0 &&
		d.ReleaseDate == "" && len(d.Videos) == 0 && len(d.Screenshots) == 0
}

// sparse reports whether the sheet shows next to nothing: no summary and no screenshots, the two
// parts that make the overview worth opening. Genres, dates or companies alone do not count. A
// sparse sheet triggers the fallback pass.
func (d GameDetails) sparse() bool { return d.Summary == "" && len(d.Screenshots) == 0 }

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

	if d.Summary == "" && o.Summary != "" {
		d.Summary, d.SummarySource, used = o.Summary, o.SummarySource, true // the credit goes with its text
	}

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

	// Trailers from every source are useful (Steam HLS, IGDB and TheGamesDB YouTube), but a YouTube
	// video two databases know is shown once.
	for _, v := range o.Videos {
		if v.YouTubeID != "" && slices.ContainsFunc(d.Videos, func(have Video) bool { return have.YouTubeID == v.YouTubeID }) {
			continue
		}

		d.Videos, used = append(d.Videos, v), true
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
		} else if ok && s.fresh(d) {
			s.registerAssets(ctx, ref, &d)
			return d, nil, nil
		}
	}

	type out struct {
		d        GameDetails
		warnings []string
	}

	v, err, _ := s.group.Do("details:"+string(id)+":"+language, func() (any, error) {
		d, w, err := s.fetchDetails(context.WithoutCancel(ctx), id, language, true)
		return out{d, w}, err
	})
	if err != nil {
		return GameDetails{}, nil, err
	}

	r := v.(out)
	s.registerAssets(ctx, ref, &r.d)
	s.enqueueAutoLink(g) // a console game may have a store sheet to find

	return r.d, r.warnings, nil
}

// fresh reports whether a cached sheet can be shown without asking the providers again: it is
// younger than detailsTTL, and not a sparse sheet gathered by an older detailsLogicVersion or one
// whose fallback pass did not complete.
func (s *Service) fresh(d GameDetails) bool {
	if d.sparse() && (d.FallbackSkipped || d.LogicVersion < detailsLogicVersion) {
		return false
	}

	return s.now().Sub(d.FetchedAt) < detailsTTL
}

// detailsPass is what one pass over the metadata chain left behind.
type detailsPass struct {
	// warnings are the failures for this game shown on the sheet; provider-level problems are the
	// providers' status instead (see ProviderStatuses).
	warnings []string

	// limited reports that a provider asked to slow down.
	limited bool

	// failed reports that a provider failed, whether the failure is shown (warnings) or a
	// provider-level problem (rejected settings).
	failed bool

	// asked holds the providers already asked, so a later pass does not ask them again.
	asked map[provider.ID]bool
}

// fetchDetails asks the metadata chain for a game's sheet and caches it. When the chain answered
// without errors but the sheet is sparse (see GameDetails.sparse), and fallback is set, it runs a
// second pass with CoverQuery.Fallback set: quota-limited providers (TheGamesDB) that keep out of
// games a store knows may then fill the gaps, for a game only a console store knows (a PS4 library
// copy) whose store has no details provider. The background scanner passes false: it must not spend
// those providers' quota.
func (s *Service) fetchDetails(ctx context.Context, id game.ID, language string, fallback bool) (GameDetails, []string, error) {
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
		Language:     language,
		FetchedAt:    s.now(),
		LogicVersion: detailsLogicVersion,
	}
	pass := detailsPass{asked: map[provider.ID]bool{}}

	s.askChain(ctx, g, chain, q, &merged, &pass)
	// The fallback runs only after a clean first pass: when a provider failed or was busy, the sheet
	// is not final (it is not cached, or cached to be asked again), so spending quota on it now would
	// be wasted.
	completed := false

	if fallback && merged.sparse() && !pass.failed && !pass.limited {
		q.Fallback = true
		s.askChain(ctx, g, chain, q, &merged, &pass)
		completed = !pass.failed && !pass.limited
	}
	// A sparse sheet whose fallback did not complete (skipped, or a provider failed in either pass)
	// may be cached, but the next opening runs the passes again instead of keeping it for detailsTTL.
	merged.FallbackSkipped = merged.sparse() && !completed
	// Cache even an empty sheet so unknown games are not looked up on every view; failures are not
	// cached. Nor is a sheet a provider was too busy to fill: cached, it would lack that provider's
	// part until the cache expires.
	if !pass.limited && (!pass.failed || !merged.empty()) {
		if err := s.details.Put(ctx, id, merged); err != nil {
			s.log.Warn("caching details", "error", err)
		}
	}

	return merged, pass.warnings, nil
}

// askChain asks every applicable provider of the chain not asked yet, filling merged's gaps.
func (s *Service) askChain(ctx context.Context, g *game.Game, chain []ProviderView, q CoverQuery, merged *GameDetails, pass *detailsPass) {
	for _, p := range chain {
		impl := s.metadata[p.ID()]
		if pass.asked[p.ID()] || !impl.Applies(q) {
			continue
		}

		pass.asked[p.ID()] = true

		d, err := impl.Details(ctx, q, merged.Language, p.Settings())
		s.noteResult(p.ID(), err)

		// Provider-level problems (busy, allowance used up, rejected settings) are the provider's
		// status, not a warning on every sheet; a rate limit still keeps the sheet out of the cache.
		if errors.Is(err, provider.ErrRateLimited) {
			pass.limited = true

			s.log.Info("metadata provider unavailable, asking again later", "provider", p.ID(), "game", g.Title(), "error", err)

			continue
		}

		if err != nil {
			pass.failed = true

			if !providerLevel(err) {
				pass.warnings = append(pass.warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			}

			s.log.Warn("metadata provider failed", "provider", p.ID(), "game", g.Title(), "error", err)

			continue
		}

		if d == nil {
			merged.NotFoundBy = append(merged.NotFoundBy, p.ID())
			continue
		}

		if merged.fill(*d) {
			merged.Sources = append(merged.Sources, p.ID())
		}
	}
}
