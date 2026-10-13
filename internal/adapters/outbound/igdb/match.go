package igdb

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// externalSources maps the stores whose links IGDB knows to its external game source ids
// (external_games.external_game_source; same values as the deprecated category enum). The uid is
// the store's own id: the Steam AppID, the GOG product id, the Epic catalog item id.
// steamSource is Steam's external game source id.
const steamSource = 1

var externalSources = []struct {
	link   string
	source int
}{
	{game.LinkSteam, steamSource},
	{game.LinkGOG, 5},
	{game.LinkEpic, 26},
}

// platformIDs maps Game Vault systems to IGDB platform ids. PC covers Windows, Mac and Linux: a PC
// store's game may be listed under any of them.
var platformIDs = map[string][]int{
	game.SystemPC:         {6, 14, 3},
	game.SystemPS5:        {167},
	game.SystemPS4:        {48},
	game.SystemPS3:        {9},
	game.SystemPS2:        {8},
	game.SystemPS1:        {7},
	game.SystemPSP:        {38},
	game.SystemPSVita:     {46},
	game.SystemXboxSeries: {169},
	game.SystemXboxOne:    {49},
	game.SystemXbox360:    {12},
	game.SystemXbox:       {11},
	game.SystemSwitch:     {130},
	game.SystemWiiU:       {41},
	game.SystemWii:        {5},
	game.SystemGameCube:   {21},
	game.SystemN64:        {4},
	game.System3DS:        {37},
	game.SystemDS:         {20},
	game.SystemGameBoy:    {33},
}

// searchSystems returns the systems a title search for q is filtered to: the edition's system, or
// without one the systems of the game's physical copies, or else of all its copies.
func searchSystems(q media.CoverQuery) []string {
	if q.System != "" {
		return []string{q.System}
	}

	platforms := q.PhysicalPlatforms
	if len(platforms) == 0 {
		platforms = q.Platforms
	}

	systems := make([]string, 0, len(platforms))
	for _, pl := range platforms {
		if s := game.SystemOf(pl); s != game.SystemOther && !slices.Contains(systems, s) {
			systems = append(systems, s)
		}
	}

	return systems
}

// gameJSON is a game as an expanded games query returns it; fields not asked for stay empty.
type gameJSON struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Summary          string `json:"summary"`
	FirstReleaseDate int64  `json:"first_release_date"`
	URL              string `json:"url"`
	VersionParent    int64  `json:"version_parent"`
	ParentGame       int64  `json:"parent_game"`
	Cover            struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	InvolvedCompanies []struct {
		Company struct {
			Name string `json:"name"`
		} `json:"company"`
		Developer bool `json:"developer"`
		Publisher bool `json:"publisher"`
	} `json:"involved_companies"`
	AgeRatings  []ageRatingJSON `json:"age_ratings"`
	Screenshots []struct {
		ImageID string `json:"image_id"`
	} `json:"screenshots"`
	Videos []struct {
		Name    string `json:"name"`
		VideoID string `json:"video_id"`
	} `json:"videos"`
	ExternalGames []struct {
		Source int    `json:"external_game_source"`
		UID    string `json:"uid"`
	} `json:"external_games"`
}

// ageRatingJSON is one rating, with its organization and category expanded.
type ageRatingJSON struct {
	Organization struct {
		Name string `json:"name"`
	} `json:"organization"`
	Category struct {
		Rating string `json:"rating"`
	} `json:"rating_category"`
}

// find returns the IGDB game q describes with the fields asked for, or nil when none matches
// clearly. It looks the game up first by its store links IGDB knows, then by its title on its
// systems.
func (c *Client) find(ctx context.Context, s schema.Settings, q media.CoverQuery, fields []string) (*gameJSON, error) {
	if g, err := c.findByLinks(ctx, s, q, fields); err != nil || g != nil {
		return g, err
	}

	return c.findByTitle(ctx, s, q, fields)
}

var reUID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// findByLinks looks the game up by its store links, in one query: the external games, each with
// its game expanded. The first store in externalSources IGDB knows wins.
func (c *Client) findByLinks(ctx context.Context, s schema.Settings, q media.CoverQuery, fields []string) (*gameJSON, error) {
	var conds []string

	for _, src := range externalSources {
		// The uid goes into the query: anything but a plain id is skipped rather than escaped.
		if uid := q.Links[src.link]; reUID.MatchString(uid) {
			conds = append(conds, fmt.Sprintf(`(external_game_source = %d & uid = "%s")`, src.source, uid))
		}
	}

	if len(conds) == 0 {
		return nil, nil
	}

	expanded := make([]string, 0, len(fields)+4)
	expanded = append(expanded, "external_game_source", "uid", "game.version_parent", "game.parent_game")

	for _, f := range fields {
		expanded = append(expanded, "game."+f)
	}

	body := fmt.Sprintf("fields %s; where %s; limit 20;", strings.Join(expanded, ","), strings.Join(conds, " | "))

	var out []struct {
		Source int       `json:"external_game_source"`
		Game   *gameJSON `json:"game"`
	}
	if err := c.query(ctx, s, "external_games", body, &out); err != nil {
		return nil, err
	}

	for _, src := range externalSources {
		var games []gameJSON

		for _, e := range out {
			if e.Source == src.source && e.Game != nil && e.Game.ID != 0 {
				games = append(games, *e.Game)
			}
		}

		if len(games) > 0 {
			// A store id IGDB lists under several games (a game and its editions) describes the
			// main one; under two main games it is not trusted, and the title search decides.
			return onlyMain(games), nil
		}
	}

	return nil, nil
}

// onlyMain returns the one game among games that is neither an edition (version_parent) nor an
// add-on (parent_game), or the only game when all are; nil when that is not a single game.
func onlyMain(games []gameJSON) *gameJSON {
	main := slices.DeleteFunc(slices.Clone(games), func(g gameJSON) bool { return g.VersionParent != 0 || g.ParentGame != 0 })
	if len(main) == 0 {
		main = games
	}

	slices.SortStableFunc(main, func(a, b gameJSON) int { return cmp.Compare(a.ID, b.ID) })

	main = slices.CompactFunc(main, func(a, b gameJSON) bool { return a.ID == b.ID })
	if len(main) != 1 {
		return nil
	}

	return &main[0]
}

// reUnsafe matches what may not appear in an Apicalypse string.
var reUnsafe = regexp.MustCompile(`["\\]+`)

// findByTitle searches the game's name on its systems and keeps a clear match: exactly the same
// title (then the same ignoring edition tags), preferring games over their editions and add-ons.
// The name is the title without edition and store tags (game.SearchTitle): IGDB names "Example
// West", not the "GOTY" disc; only when that finds no match is the raw title searched too.
// Two different games with that title (a 1993 original and its 2016 reboot) give nothing: a wrong
// sheet is worse than none.
func (c *Client) findByTitle(ctx context.Context, s schema.Settings, q media.CoverQuery, fields []string) (*gameJSON, error) {
	clean := func(t string) string { return strings.Join(strings.Fields(reUnsafe.ReplaceAllString(t, " ")), " ") }

	terms := []string{clean(game.SearchTitle(q.Title))}
	if raw := clean(game.NormalizeTitle(q.Title)); raw != terms[0] { // a ™ alone is no other title
		terms = append(terms, raw)
	}

	for _, term := range terms {
		if term == "" {
			continue
		}

		g, err := c.searchTitle(ctx, s, q, term, fields)
		if err != nil || g != nil {
			return g, err
		}
	}

	return nil, nil
}

// searchTitle runs one title search for term on q's systems and picks q's game among the results.
func (c *Client) searchTitle(ctx context.Context, s schema.Settings, q media.CoverQuery, term string, fields []string) (*gameJSON, error) {
	all := append(slices.Clone(fields), "version_parent", "parent_game")
	body := fmt.Sprintf(`search "%s"; fields %s;`, term, strings.Join(all, ","))

	var ids []string

	for _, system := range searchSystems(q) {
		for _, id := range platformIDs[system] {
			ids = append(ids, strconv.Itoa(id))
		}
	}

	if len(ids) > 0 {
		body += fmt.Sprintf(" where platforms = (%s);", strings.Join(ids, ","))
	}

	body += " limit 20;"

	var out []gameJSON
	if err := c.query(ctx, s, "games", body, &out); err != nil {
		return nil, err
	}

	return pick(out, q.Title), nil
}

// pick chooses the game titled title among search results (see findByTitle).
func pick(games []gameJSON, title string) *gameJSON {
	for _, same := range []func(string) bool{
		func(t string) bool { return game.SameTitle(t, title) },
		func(t string) bool { return game.SameMatchKey(t, title) },
	} {
		var matches []gameJSON

		for _, g := range games {
			if same(g.Name) {
				matches = append(matches, g)
			}
		}

		if len(matches) == 0 {
			continue
		}

		// An edition (version_parent) or an add-on (parent_game) shares its game's title: the
		// game itself is the one to describe.
		return onlyMain(matches)
	}

	return nil
}

// imageURL is the address of an IGDB image in a size ("cover_big", "screenshot_big", "1080p").
func imageURL(size, imageID string) string {
	return "https://images.igdb.com/igdb/image/upload/t_" + size + "/" + imageID + ".jpg"
}
