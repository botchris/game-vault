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

// steamSource is Steam's external game source id.
const steamSource = 1

// externalSources maps the stores whose links IGDB knows to its external game source ids
// (external_games.external_game_source; same values as the deprecated category enum). The uid is
// the store's own id: the Steam AppID, the GOG product id, the Epic catalog item id.
var externalSources = []struct {
	link   string
	source int
}{
	{game.LinkSteam, steamSource},
	{game.LinkGOG, 5},
	{game.LinkEpic, 26},
}

// platformIDs maps every Game Vault system to its IGDB platform ids. PC covers Windows, Mac and Linux: a PC
// store's game may be listed under any of them.
var platformIDs = map[string][]int{
	game.SystemPC:                  {6, 14, 3},
	game.SystemPS5:                 {167},
	game.SystemPS4:                 {48},
	game.SystemPS3:                 {9},
	game.SystemPS2:                 {8},
	game.SystemPS1:                 {7},
	game.SystemPSVita:              {46},
	game.SystemPSP:                 {38},
	game.SystemXboxSeries:          {169},
	game.SystemXboxOne:             {49},
	game.SystemXbox360:             {12},
	game.SystemXbox:                {11},
	"Switch 2":                     {508},
	game.SystemSwitch:              {130},
	game.SystemWiiU:                {41},
	game.SystemWii:                 {5},
	game.SystemGameCube:            {21},
	"Nintendo 64DD":                {416},
	game.SystemN64:                 {4},
	"SNES":                         {19, 58},
	"Famicom Disk System":          {51},
	"NES":                          {18, 99},
	game.System3DS:                 {37},
	game.SystemDS:                  {20},
	"Game Boy Advance":             {24},
	"Pokémon Mini":                 {166},
	"Game Boy Color":               {22},
	"Virtual Boy":                  {87},
	game.SystemGameBoy:             {33},
	"Game & Watch":                 {307},
	"Dreamcast":                    {23},
	"32X":                          {30},
	"Saturn":                       {32},
	"Pico":                         {339},
	"Mega-CD / Sega CD":            {78},
	"Mega Drive / Genesis":         {29},
	"Master System":                {64},
	"SG-1000":                      {84},
	"Game Gear":                    {35},
	"Atari Jaguar CD":              {410},
	"Atari Jaguar":                 {62},
	"Atari 7800":                   {60},
	"Atari 5200":                   {66},
	"Atari 2600":                   {59},
	"Atari Lynx":                   {61},
	"Atari ST":                     {63},
	"Atari 8-bit":                  {65},
	"PC-FX":                        {274},
	"SuperGrafx":                   {128},
	"PC Engine CD / TurboGrafx-CD": {150},
	"PC Engine / TurboGrafx-16":    {86},
	"PC-98":                        {149},
	"PC-88":                        {125},
	"Neo Geo CD":                   {136},
	"Neo Geo AES":                  {80, 79},
	"Neo Geo Pocket Color":         {120},
	"Neo Geo Pocket":               {119},
	"Playdia":                      {308},
	"WonderSwan Color":             {123},
	"WonderSwan":                   {57},
	"Acorn Archimedes":             {116},
	"Acorn Electron":               {134},
	"BBC Micro":                    {69},
	"Amstrad GX4000":               {506},
	"Amstrad CPC":                  {25},
	"Apple Pippin":                 {476},
	"Apple II":                     {75},
	"Evercade":                     {309},
	"ColecoVision":                 {68},
	"Amiga CD32":                   {114},
	"Commodore CDTV":               {158},
	"Amiga":                        {16},
	"Commodore 16 / Plus/4":        {93, 94},
	"Commodore 64":                 {15},
	"VIC-20":                       {71},
	"Commodore PET":                {90},
	"Dragon 32/64":                 {153},
	"Channel F":                    {127},
	"FM Towns":                     {118},
	"Fujitsu FM-7":                 {152},
	"Vectrex":                      {70},
	"MSX2":                         {53},
	"MSX":                          {27},
	"Odyssey 2":                    {133},
	"Magnavox Odyssey":             {88},
	"HyperScan":                    {407},
	"Intellivision":                {67},
	"N-Gage":                       {42},
	"Ouya":                         {72},
	"3DO":                          {50},
	"Playdate":                     {381},
	"CD-i":                         {117},
	"Sharp X68000":                 {121},
	"Sharp X1":                     {77},
	"Sinclair QL":                  {406},
	"ZX Spectrum":                  {26},
	"ZX81":                         {373},
	"Game.com":                     {379},
	"R-Zone":                       {475},
	"V.Smile":                      {439},
	"Nuon":                         {122},
	"Casio Loopy":                  {380},
	"LaserActive":                  {487},
	"Super Cassette Vision":        {376},
	"Emerson Arcadia 2001":         {473},
	"Epoch Cassette Vision":        {375},
	"Interton VC 4000":             {138},
	"Bally Astrocade":              {91},
	"Gizmondo":                     {474},
	"Tapwave Zodiac":               {44},
	"Mega Duck":                    {408},
	"Watara Supervision":           {415},
	"Gamate":                       {378},
	"Microvision":                  {89},
	"Thomson MO5":                  {156},
	"Texas Instruments TI-99/4A":   {129},
	"TRS-80 Color Computer":        {151},
}

// unmappedSystems are the catalog systems IGDB has no platform of its own for.
var unmappedSystems = []string{"Mac (classic)"}

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
func (c *Client) find(ctx context.Context, s schema.Settings, q media.GameQuery, fields []string) (*gameJSON, error) {
	if g, err := c.findByLinks(ctx, s, q, fields); err != nil || g != nil {
		return g, err
	}

	return c.findByTitle(ctx, s, q, fields)
}

var reUID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// findByLinks looks the game up by its store links, in one query: the external games, each with
// its game expanded. The first store in externalSources IGDB knows wins.
func (c *Client) findByLinks(ctx context.Context, s schema.Settings, q media.GameQuery, fields []string) (*gameJSON, error) {
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
func (c *Client) findByTitle(ctx context.Context, s schema.Settings, q media.GameQuery, fields []string) (*gameJSON, error) {
	clean := func(t string) string { return strings.Join(strings.Fields(reUnsafe.ReplaceAllString(t, " ")), " ") }

	var terms []string

	for _, t := range game.SearchTerms(q.Title) {
		if t = clean(t); t != "" && !slices.Contains(terms, t) { // IGDB's unsafe characters gone, two may be one
			terms = append(terms, t)
		}
	}

	for _, term := range terms {
		g, err := c.searchTitle(ctx, s, q, term, fields)
		if err != nil || g != nil {
			return g, err
		}
	}

	return nil, nil
}

// searchTitle runs one title search for term on q's systems and picks q's game among the results.
func (c *Client) searchTitle(ctx context.Context, s schema.Settings, q media.GameQuery, term string, fields []string) (*gameJSON, error) {
	all := append(slices.Clone(fields), "version_parent", "parent_game")
	body := fmt.Sprintf(`search "%s"; fields %s;`, term, strings.Join(all, ","))

	var ids []string

	for _, system := range q.SearchSystems() {
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
