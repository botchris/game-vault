package media

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"time"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// Video is a trailer: an HLS stream (Steam) or a YouTube video (IGDB, TheGamesDB).
type Video struct {
	Title     string
	Thumbnail string
	HLSURL    string
	YouTubeID string

	// ThumbAsset is the local name of the downloaded thumbnail (see Assets.Asset).
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
	providers.Provider

	// Applies reports, without any network call, whether the provider can know this game.
	Applies(q GameQuery) bool

	// Details returns what the provider knows in the given UI language ("en", "es"...);
	// nil when it does not know the game.
	Details(ctx context.Context, q GameQuery, language string, settings schema.Settings) (*GameDetails, error)
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

// noDetails is the DetailsStore of a server that caches no sheet: every sheet is gathered again.
type noDetails struct{}

func (noDetails) Get(context.Context, game.ID, string) (GameDetails, bool, error) {
	return GameDetails{}, false, nil
}

func (noDetails) Put(context.Context, game.ID, GameDetails) error { return nil }

func (noDetails) Delete(context.Context, game.ID) error { return nil }

func (noDetails) Summaries(context.Context, string) (map[game.ID]Summary, error) {
	return map[game.ID]Summary{}, nil
}
