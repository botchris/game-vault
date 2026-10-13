package playstation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
)

// The library website lists apps (streaming, music), demos, soundtracks and videos as PS4 / PS5
// titles with CUSA… / PPSA… ids, and nothing in its answer tells them apart from games. Two
// signals do, and each title they flag is withdrawn instead of imported:
//
//   - the PlayStation app's played-titles list, which gives each title a category: a purchased
//     title that was played and is listed among all played titles but not among the played games
//     is not a game. Titles never played are unknown and stay;
//   - names that clearly mark a non-game ("… Demo", "… Original Soundtrack", "Behind the Scenes").
//
// The store catalog would know every title's kind, but playstation.com's terms forbid automated
// access to its pages, so it is not read.

const (
	defaultGameListURL = "https://m.np.playstation.com/api/gamelist/v2/users/me/titles"

	// gameCategories are the played-title categories that are games.
	gameCategories = "ps4_game,ps5_native_game,pspc_game"

	gameListPageSize = 200

	// maxGameListPages caps the played-titles paging; a list longer than that is not trusted.
	maxGameListPages = 50
)

// flexID is an id that Sony may send as a number or as a string.
type flexID string

// UnmarshalJSON accepts a JSON number or string.
func (f *flexID) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return nil
	}

	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexID(s)
		return nil
	}

	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}

	*f = flexID(n.String())

	return nil
}

// playedTitle is one title of the played-titles list (the fields Game Vault reads).
type playedTitle struct {
	TitleID  string `json:"titleId"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Concept  *struct {
		ID       flexID   `json:"id"`
		TitleIDs []string `json:"titleIds"`
	} `json:"concept"`
}

// ids returns the title ids the played title stands for: its own and its concept's.
func (t playedTitle) ids() []string {
	ids := []string{t.TitleID}
	if t.Concept != nil {
		ids = append(ids, t.Concept.TitleIDs...)
	}

	return ids
}

func (t playedTitle) conceptID() string {
	if t.Concept == nil {
		return ""
	}

	return string(t.Concept.ID)
}

// errGameListPages means the played-titles list did not end within maxGameListPages pages.
var errGameListPages = errors.New("playstation played titles: too many pages")

// playedTitles reads every played title of the account, only those of the given categories when
// categories is not empty.
func (p *Provider) playedTitles(ctx context.Context, token, categories string) ([]playedTitle, error) {
	var all []playedTitle

	offset := 0

	for range maxGameListPages {
		q := url.Values{"limit": {strconv.Itoa(gameListPageSize)}, "offset": {strconv.Itoa(offset)}}
		if categories != "" {
			q.Set("categories", categories)
		}

		titles, next, err := p.gameListPage(ctx, token, q)
		if err != nil {
			return nil, err
		}

		all = append(all, titles...)

		if next == nil || len(titles) == 0 || *next <= offset {
			return all, nil
		}

		offset = *next
	}

	return nil, errGameListPages
}

func (p *Provider) gameListPage(ctx context.Context, token string, q url.Values) ([]playedTitle, *int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.GameListURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", apiclient.BrowserUserAgent)

	res, err := p.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))

	var out struct {
		Titles     []playedTitle `json:"titles"`
		NextOffset *int          `json:"nextOffset"`
		Error      *struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"error"`
	}

	jsonErr := json.Unmarshal(raw, &out)

	switch {
	case out.Error != nil:
		return nil, nil, fmt.Errorf("playstation played titles: HTTP %d, %s %s", res.StatusCode, out.Error.Reason, out.Error.Message)
	case res.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("playstation played titles: HTTP %d", res.StatusCode)
	case jsonErr != nil:
		return nil, nil, fmt.Errorf("playstation played titles: unexpected answer: %w", jsonErr)
	}

	return out.Titles, out.NextOffset, nil
}

// nonGameIndex holds the title and concept ids of played titles that are not games.
type nonGameIndex struct {
	titles   map[string]bool
	concepts map[string]bool
}

// newNonGameIndex finds the titles listed among all played titles but not among the played
// games. An id that also belongs to a played game is never taken as a non-game's. When the
// unfiltered list only holds games, nothing is flagged.
func newNonGameIndex(all, games []playedTitle) nonGameIndex {
	gameTitles := map[string]bool{}
	gameConcepts := map[string]bool{}

	for _, g := range games {
		for _, id := range g.ids() {
			gameTitles[id] = true
		}

		if c := g.conceptID(); c != "" {
			gameConcepts[c] = true
		}
	}

	idx := nonGameIndex{
		titles:   map[string]bool{},
		concepts: map[string]bool{},
	}

	for _, t := range all {
		if t.TitleID == "" || gameTitles[t.TitleID] {
			continue
		}

		for _, id := range t.ids() {
			if id != "" && !gameTitles[id] {
				idx.titles[id] = true
			}
		}

		if c := t.conceptID(); c != "" && !gameConcepts[c] {
			idx.concepts[c] = true
		}
	}

	return idx
}

// has reports whether the purchased title is a played title that is not a game.
func (idx nonGameIndex) has(t title) bool {
	return (t.TitleID != "" && idx.titles[t.TitleID]) || (t.ConceptID != "" && idx.concepts[t.ConceptID])
}

// playedNonGames reads both played-titles lists and indexes the non-games. The lists only refine
// the import, so when either cannot be read the scan goes on without them (nil).
func (p *Provider) playedNonGames(ctx context.Context, token string) *nonGameIndex {
	all, err := p.playedTitles(ctx, token, "")
	if err == nil {
		var games []playedTitle

		games, err = p.playedTitles(ctx, token, gameCategories)
		if err == nil {
			idx := newNonGameIndex(all, games)
			return &idx
		}
	}

	if p.Log != nil {
		p.Log.Warn("playstation: played titles not read, apps and videos are not told apart from games", "error", err)
	}

	return nil
}

// nonGameMarkers are the words and phrases that mark a non-game when a name ends with them, in
// English and Spanish (account languages vary).
var nonGameMarkers = strings.Join([]string{
	`demo`,
	`trial`, `trial version`, `free trial`,
	`beta`, `open beta`, `closed beta`,
	`soundtrack`, `original soundtrack`, `ost`,
	`theme`, `dynamic theme`,
	`avatars?`,
	`art ?book`, `digital art ?book`,
	`versi[oó]n de prueba`, `prueba gratuita`,
	`banda sonora`, `banda sonora original`,
	`tema`, `tema din[aá]mico`,
	`avatar(?:es)`, // the Spanish plural, spelled this way so the English spell check leaves it alone
}, "|")

var (
	// reTrailingMarker matches a marker as the last word or phrase, after a space, a dash, a colon
	// or an en dash, or inside trailing parentheses or brackets.
	reTrailingMarker = regexp.MustCompile(`(?i)(?:[\s:\-–—](?:` + nonGameMarkers + `)|[(\[]\s*(?:` + nonGameMarkers + `)\s*[)\]])$`)

	// reAnywhereMarker matches the phrases that mark a video wherever they are.
	reAnywhereMarker = regexp.MustCompile(`(?i)\b(?:behind the scenes|making of)\b`)
)

// nonGameName reports whether a title's name clearly marks it as something other than a game: a
// demo, trial, beta, soundtrack, theme, avatar, art book or making-of video. A marker word that
// is part of a real title ("Theme Park Rush", "Trial of the Lantern") does not count.
func nonGameName(name string) bool {
	name = strings.TrimSpace(name)

	return reTrailingMarker.MatchString(name) || reAnywhereMarker.MatchString(name)
}
