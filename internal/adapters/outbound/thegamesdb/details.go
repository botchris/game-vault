package thegamesdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// DetailsID identifies TheGamesDB in the metadata chain. It shares the API key with the cover
// provider (same settings group).
const DetailsID provider.ID = "thegamesdb-details"

// Details implements media.MetadataProvider: platform-specific overview, genres, companies, age
// rating, players and YouTube trailer. Texts are in English.
type Details struct {
	p *Provider

	mu    sync.Mutex
	names map[string]map[int64]string // "genres" | "developers" | "publishers" → id → name
}

var _ media.MetadataProvider = (*Details)(nil)

// NewDetails builds the metadata provider on top of the cover provider (shared platform cache).
func NewDetails(p *Provider) *Details {
	return &Details{
		p:     p,
		names: map[string]map[int64]string{},
	}
}

// Descriptor implements media.MetadataProvider.
func (d *Details) Descriptor() provider.Descriptor {
	desc := d.p.Descriptor()

	return provider.Descriptor{
		ID:             DetailsID,
		Kind:           provider.KindMetadata,
		Name:           "TheGamesDB",
		DescriptionKey: "providers.thegamesdbDetails.description",
		Fields:         desc.Fields,
		SettingsGroup:  string(ID),
		DefaultOrder:   20,
	}
}

// Test implements media.Provider: it checks the shared API key and its allowance, exactly as the
// cover provider does, because both use the same key.
func (d *Details) Test(ctx context.Context, s schema.Settings) error { return d.p.Test(ctx, s) }

// Applies implements media.MetadataProvider.
func (d *Details) Applies(q media.CoverQuery) bool { return d.p.Applies(q) }

// ImageHosts implements media.ImageHoster: screenshots and YouTube thumbnails go through the proxy.
func (d *Details) ImageHosts() []string { return []string{"cdn.thegamesdb.net", "i.ytimg.com"} }

// lookupNames resolves genre/developer/publisher ids, fetching each list once per process.
func (d *Details) lookupNames(ctx context.Context, apiKey, kind string, ids []int64) []string {
	d.mu.Lock()
	table, ok := d.names[kind]
	d.mu.Unlock()

	if !ok {
		// data holds {"count": N, "<kind>": {"<id>": {"id": …, "name": …}}}: decode the map alone.
		var out struct {
			Data map[string]json.RawMessage `json:"data"`
		}

		path := "/v1/" + strings.ToUpper(kind[:1]) + kind[1:]
		if err := d.p.get(ctx, path, url.Values{"apikey": {apiKey}}, &out); err != nil {
			return nil
		}

		var items map[string]struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(out.Data[kind], &items); err != nil {
			return nil
		}

		table = map[int64]string{}
		for _, item := range items {
			table[item.ID] = item.Name
		}

		d.mu.Lock()
		d.names[kind] = table
		d.mu.Unlock()
	}

	var names []string

	for _, id := range ids {
		if n := table[id]; n != "" {
			names = append(names, n)
		}
	}

	return names
}

// find searches the game's name, then its raw title (game.SearchTerms) unless the name found a game
// titled exactly like the copy, on the systems the query is about, and picks the game in the
// results: an exact title wins over a game that only shares the MatchKey ("Rush" for "Gold Rush!",
// searched as "Rush!"), whichever search found it.
func (d *Details) find(ctx context.Context, key string, q media.CoverQuery) (tgdbGame, bool, error) {
	filter := d.p.platformFilter(ctx, key, q.SearchSystems())

	var (
		best  tgdbGame
		found bool
	)

	for _, term := range game.SearchTerms(q.Title) {
		params := url.Values{"apikey": {key}, "name": {term}, "fields": {"overview,players,publishers,genres,rating,coop,youtube"}}
		if filter != "" {
			params.Set("filter[platform]", filter)
		}

		var out struct {
			Data struct {
				Games []tgdbGame `json:"games"`
			} `json:"data"`
		}
		if err := d.p.get(ctx, "/v1.1/Games/ByGameName", params, &out); err != nil {
			return tgdbGame{}, false, err
		}

		g, ok := pickGame(out.Data.Games, q.Title)
		if ok && game.SameTitle(g.Title, q.Title) {
			return g, true, nil // pickGame prefers exact titles
		}

		if ok && !found {
			best, found = g, true
		}
	}

	return best, found, nil
}

// Details implements media.MetadataProvider.
func (d *Details) Details(ctx context.Context, q media.CoverQuery, _ string, s schema.Settings) (*media.GameDetails, error) {
	key := s[settingAPIKey]

	g, ok, err := d.find(ctx, key, q)
	if err != nil || !ok {
		return nil, err
	}

	det := &media.GameDetails{
		Summary:     strings.TrimSpace(g.Overview),
		ReleaseDate: g.ReleaseDate,
		AgeRating:   cleanRating(g.Rating),
		StoreURL:    fmt.Sprintf("https://thegamesdb.net/game.php?id=%d", g.ID),
		Genres:      d.lookupNames(ctx, key, "genres", g.Genres),
		Developers:  d.lookupNames(ctx, key, "developers", g.Developers),
		Publishers:  d.lookupNames(ctx, key, "publishers", g.Publishers),
	}
	if g.Players > 0 {
		det.Players = strconv.Itoa(g.Players)
		if strings.EqualFold(g.Coop, "yes") {
			det.Players += " · co-op"
		}
	}

	if yt := youTubeID(g.YouTube); yt != "" {
		det.Videos = []media.Video{{
			Title:     g.Title,
			YouTubeID: yt,
			Thumbnail: "https://i.ytimg.com/vi/" + yt + "/hqdefault.jpg",
		}}
	}

	det.Screenshots = d.screenshots(ctx, key, g.ID)

	return det, nil
}

func (d *Details) screenshots(ctx context.Context, key string, gameID int64) []media.Screenshot {
	var out struct {
		Data struct {
			BaseURL map[string]string `json:"base_url"`
			Images  map[string][]struct {
				Filename string `json:"filename"`
			} `json:"images"`
		} `json:"data"`
	}

	id := strconv.FormatInt(gameID, 10)
	if err := d.p.get(ctx, "/v1/Games/Images", url.Values{"apikey": {key}, "games_id": {id}, "filter[type]": {"screenshot"}}, &out); err != nil {
		return nil
	}

	var shots []media.Screenshot
	for _, img := range out.Data.Images[id] {
		if len(shots) == maxScreenshots {
			break
		}

		shots = append(shots, media.Screenshot{
			ThumbURL: out.Data.BaseURL["medium"] + img.Filename,
			FullURL:  out.Data.BaseURL["original"] + img.Filename,
		})
	}

	return shots
}

const maxScreenshots = 8

// youTubeID accepts a bare id or a full YouTube URL.
func youTubeID(v string) string {
	v = strings.TrimSpace(v)
	if u, err := url.Parse(v); err == nil && u.Host != "" {
		if id := u.Query().Get("v"); id != "" {
			return id
		}

		return strings.Trim(u.Path, "/")
	}

	for _, r := range v {
		if r != '-' && r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return ""
		}
	}

	return v
}

type tgdbGame struct {
	ID          int64   `json:"id"`
	Title       string  `json:"game_title"`
	ReleaseDate string  `json:"release_date"`
	Overview    string  `json:"overview"`
	Players     int     `json:"players"`
	Coop        string  `json:"coop"`
	Rating      string  `json:"rating"`
	YouTube     string  `json:"youtube"`
	Developers  []int64 `json:"developers"`
	Genres      []int64 `json:"genres"`
	Publishers  []int64 `json:"publishers"`
}

// richness scores how complete an entry is: TheGamesDB often has several entries with the same
// title and platform (regional releases, re-releases), and some are almost empty.
func (g tgdbGame) richness() int {
	n := 0
	if g.Overview != "" {
		n += 3
	}

	if g.YouTube != "" {
		n += 2
	}

	for _, l := range [][]int64{g.Genres, g.Developers, g.Publishers} {
		if len(l) > 0 {
			n++
		}
	}

	if cleanRating(g.Rating) != "" {
		n++
	}

	return n
}

// pickGame chooses, among entries with exactly the same title (then the same title ignoring
// edition tags), the most complete one. Entries for other games are never used.
func pickGame(games []tgdbGame, title string) (tgdbGame, bool) {
	for _, same := range []func(string) bool{
		func(t string) bool { return game.SameTitle(t, title) },
		func(t string) bool { return game.SameMatchKey(t, title) },
	} {
		best, found := tgdbGame{}, false
		for _, g := range games {
			if same(g.Title) && (!found || g.richness() > best.richness()) {
				best, found = g, true
			}
		}

		if found {
			return best, true
		}
	}

	return tgdbGame{}, false
}

func cleanRating(r string) string {
	if strings.EqualFold(strings.TrimSpace(r), "not rated") {
		return ""
	}

	return strings.TrimSpace(r)
}
