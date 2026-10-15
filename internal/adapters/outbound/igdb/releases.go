package igdb

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ReleasesID identifies IGDB in the releases chain. It shares the Twitch credentials with the cover
// and details providers (same settings group).
const ReleasesID provider.ID = "igdb-releases"

const (
	// maxAnticipated is how many anticipated releases are asked for.
	maxAnticipated = 40

	// maxBatch is IGDB's largest limit: Steam apps and series ids are sent this many at a time.
	maxBatch = 500

	// testWindow is the window the home page asks the anticipated releases for, so Test sends the
	// query a refresh sends and never a shape of its own.
	testWindow = 60 * 24 * time.Hour
)

// releaseFields are the game fields a release card needs. Platforms stay ids: they are mapped to
// Game Vault's systems, unknown ones left out.
const releaseFields = "name,first_release_date,url,cover.image_id,platforms,franchise,franchises,collections"

// mainGames keeps editions and add-ons out: a deluxe edition or a DLC is not a new game.
const mainGames = "version_parent = null & parent_game = null"

// Releases implements home.ReleasesProvider: the most anticipated games of the next weeks, and the
// upcoming entries of the series the user's games belong to.
type Releases struct{ c *Client }

var (
	_ home.ReleasesProvider = (*Releases)(nil)
	_ media.ImageHoster     = (*Releases)(nil)
)

// NewReleases builds the releases provider on c, shared with the cover and details providers.
func NewReleases(c *Client) *Releases { return &Releases{c: c} }

// Descriptor implements providers.Impl.
func (r *Releases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ReleasesID,
		Kind:           provider.KindReleases,
		Name:           "IGDB",
		DescriptionKey: "providers.igdbReleases.description",
		Fields:         fields(),
		SettingsGroup:  string(CoversID),
		// Off until the user turns it on, even with the Twitch application entered: it runs on a
		// schedule (provider.Kind.Scheduled).
		DefaultOrder: 10,
	}
}

// Test implements providers.Impl with a new access token and the anticipated releases query, as a
// refresh sends it.
func (r *Releases) Test(ctx context.Context, s schema.Settings) error {
	var out []releaseJSON

	return r.c.testWith(ctx, s, "games", r.anticipatedQuery(testWindow), &out)
}

// ImageHosts implements media.ImageHoster.
func (r *Releases) ImageHosts() []string { return []string{"images.igdb.com"} }

// releaseJSON is a game as the release queries return it.
type releaseJSON struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	FirstReleaseDate int64  `json:"first_release_date"`
	URL              string `json:"url"`
	Cover            struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Platforms   []int   `json:"platforms"`
	Franchise   int64   `json:"franchise"`
	Franchises  []int64 `json:"franchises"`
	Collections []int64 `json:"collections"`
}

// Anticipated implements home.ReleasesProvider with one query: main games whose first release falls
// within window, with followers before release, most followed first.
func (r *Releases) Anticipated(ctx context.Context, s schema.Settings, window time.Duration) ([]game.Release, error) {
	var out []releaseJSON
	if err := r.c.query(ctx, s, "games", r.anticipatedQuery(window), &out); err != nil {
		return nil, err
	}

	releases := make([]game.Release, 0, len(out))
	for _, g := range out {
		releases = append(releases, g.release())
	}

	return releases, nil
}

// anticipatedQuery asks for the main games whose first release falls within window from now, with
// followers before release, most followed first.
func (r *Releases) anticipatedQuery(window time.Duration) string {
	from := r.c.now().Unix()
	to := from + int64(window/time.Second)

	return fmt.Sprintf("fields %s; where first_release_date >= %d & first_release_date < %d & hypes > 0 & %s; sort hypes desc; limit %d;",
		releaseFields, from, to, mainGames, maxAnticipated)
}

// series is what the owned games' series map to: the owned game that explains each.
type series struct {
	collections map[int64]home.OwnedRef
	franchises  map[int64]home.OwnedRef

	// owned holds the IGDB ids of the owned games, so they are never listed as upcoming.
	owned map[int64]bool
}

// Sequels implements home.ReleasesProvider. The owned games are found by their Steam apps (IGDB's
// external games, source 1): the details cache keeps no IGDB ids, and Steam ids are the store ids
// whose format IGDB was checked against. Games without a Steam link do not contribute.
func (r *Releases) Sequels(ctx context.Context, s schema.Settings, owned []home.OwnedRef) ([]game.Release, error) {
	byApp := map[string]home.OwnedRef{}

	var apps []string

	for _, o := range owned {
		// The uid goes into the query: anything but a plain id is skipped rather than escaped.
		if app := o.Links[game.LinkSteam]; reUID.MatchString(app) && byApp[app].Game == "" {
			byApp[app] = o
			apps = append(apps, app)
		}
	}

	if len(apps) == 0 {
		return nil, nil
	}

	ser, err := r.seriesOf(ctx, s, apps, byApp)
	if err != nil || (len(ser.collections) == 0 && len(ser.franchises) == 0) {
		// No series known: an empty id list would be a syntax error, and there is nothing to ask.
		return nil, err
	}

	return r.upcoming(ctx, s, ser)
}

// seriesOf finds the collections and franchises of the owned games, in batches of maxBatch apps.
// The owned game that explains a series is the first in apps (the owned games' order) to belong to
// it, whatever order IGDB lists them in, so it does not change from one refresh to the next.
func (r *Releases) seriesOf(ctx context.Context, s schema.Settings, apps []string, byApp map[string]home.OwnedRef) (series, error) {
	ser := series{
		collections: map[int64]home.OwnedRef{},
		franchises:  map[int64]home.OwnedRef{},
		owned:       map[int64]bool{},
	}

	gamesOf := map[string][]*releaseJSON{}

	for batch := range slices.Chunk(apps, maxBatch) {
		if err := r.externalGames(ctx, s, batch, gamesOf); err != nil {
			return series{}, err
		}
	}

	for _, app := range apps {
		ref := byApp[app]

		for _, g := range gamesOf[app] {
			ser.owned[g.ID] = true

			for _, id := range g.Collections {
				if _, seen := ser.collections[id]; !seen {
					ser.collections[id] = ref
				}
			}

			for _, id := range append(slices.Clone(g.Franchises), g.Franchise) {
				if _, seen := ser.franchises[id]; id != 0 && !seen {
					ser.franchises[id] = ref
				}
			}
		}
	}

	return ser, nil
}

// externalGames adds the IGDB games of the Steam apps to gamesOf. One app can be listed under
// several games (editions, re-releases), so a batch can have more rows than apps: pages follow
// while they come back full, sorted by id so that paging neither skips nor repeats a row.
func (r *Releases) externalGames(ctx context.Context, s schema.Settings, apps []string, gamesOf map[string][]*releaseJSON) error {
	quoted := make([]string, len(apps))
	for i, a := range apps {
		quoted[i] = strconv.Quote(a)
	}

	for offset := 0; ; offset += maxBatch {
		body := fmt.Sprintf("fields uid,game.franchise,game.franchises,game.collections; where external_game_source = %d & uid = (%s); sort id asc; limit %d; offset %d;",
			steamSource, strings.Join(quoted, ","), maxBatch, offset)

		var out []struct {
			UID  string       `json:"uid"`
			Game *releaseJSON `json:"game"`
		}
		if err := r.c.query(ctx, s, "external_games", body, &out); err != nil {
			return err
		}

		for _, e := range out {
			if e.Game != nil {
				gamesOf[e.UID] = append(gamesOf[e.UID], e.Game)
			}
		}

		if len(out) < maxBatch {
			return nil
		}
	}
}

// upcoming asks for the main games of the series that come out after now, soonest first, in
// batches of maxBatch ids of each kind, and names the owned game each continues: its collection's
// (the closer relation) before its franchise's.
func (r *Releases) upcoming(ctx context.Context, s schema.Settings, ser series) ([]game.Release, error) {
	collections := sortedKeys(ser.collections)
	franchises := sortedKeys(ser.franchises)
	now := r.c.now().Unix()

	var (
		out  []game.Release
		seen = map[int64]bool{}
	)

	for i := 0; i < max(len(collections), len(franchises)); i += maxBatch {
		var conds []string

		if ids := window(collections, i); ids != "" {
			conds = append(conds, "collections = ("+ids+")")
		}

		if ids := window(franchises, i); ids != "" {
			conds = append(conds, "franchises = ("+ids+") | franchise = ("+ids+")")
		}

		body := fmt.Sprintf("fields %s; where (%s) & first_release_date > %d & %s; sort first_release_date asc; limit %d;",
			releaseFields, strings.Join(conds, " | "), now, mainGames, maxBatch)

		var games []releaseJSON
		if err := r.c.query(ctx, s, "games", body, &games); err != nil {
			return nil, err
		}

		for _, g := range games {
			if ser.owned[g.ID] || seen[g.ID] {
				continue
			}

			ref, ok := because(g, ser)
			if !ok {
				continue
			}

			seen[g.ID] = true
			rel := g.release()
			rel.Because, rel.BecauseTitle = ref.Game, ref.Title
			out = append(out, rel)
		}
	}

	slices.SortStableFunc(out, func(a, b game.Release) int { return cmp.Compare(a.Date, b.Date) })

	return out, nil
}

// because returns the owned game a release continues: through a shared collection first.
func because(g releaseJSON, ser series) (home.OwnedRef, bool) {
	for _, id := range g.Collections {
		if ref, ok := ser.collections[id]; ok {
			return ref, true
		}
	}

	for _, id := range append(slices.Clone(g.Franchises), g.Franchise) {
		if ref, ok := ser.franchises[id]; ok {
			return ref, true
		}
	}

	return home.OwnedRef{}, false
}

// sortedKeys returns the ids in increasing order, so queries are the same from one day to the next.
func sortedKeys(m map[int64]home.OwnedRef) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}

	slices.Sort(out)

	return out
}

// window returns ids[i:i+maxBatch] joined by commas, "" when that part is empty.
func window(ids []int64, i int) string {
	if i >= len(ids) {
		return ""
	}

	part := ids[i:min(i+maxBatch, len(ids))]
	out := make([]string, len(part))

	for j, id := range part {
		out[j] = strconv.FormatInt(id, 10)
	}

	return strings.Join(out, ",")
}

// systemByPlatform maps IGDB platform ids back to Game Vault systems.
var systemByPlatform = func() map[int]string {
	out := map[int]string{}

	for system, ids := range platformIDs {
		for _, id := range ids {
			out[id] = system
		}
	}

	return out
}()

// release returns the game as a game.Release; Because is the caller's to fill.
func (g releaseJSON) release() game.Release {
	out := game.Release{
		ProviderGameID: strconv.FormatInt(g.ID, 10),
		Title:          strings.TrimSpace(g.Name),
		URL:            g.URL,
	}

	if g.FirstReleaseDate != 0 {
		out.Date = game.DateOf(time.Unix(g.FirstReleaseDate, 0).UTC())
	}

	if g.Cover.ImageID != "" {
		out.CoverURL = imageURL("cover_big", g.Cover.ImageID)
	}

	for _, id := range g.Platforms {
		if system, ok := systemByPlatform[id]; ok && !slices.Contains(out.Platforms, system) {
			out.Platforms = append(out.Platforms, system)
		}
	}

	return out
}
