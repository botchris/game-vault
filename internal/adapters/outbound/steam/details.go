package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// DetailsProviderID identifies the Steam store in the metadata chain.
const DetailsProviderID provider.ID = "steam-details"

// steamLanguages maps UI languages to the store's `l` parameter.
var steamLanguages = map[string]string{"en": "english", "es": "spanish", "fr": "french", "de": "german", "it": "italian", "pt": "portuguese"}

// Details implements media.MetadataProvider with the public store API (no key needed), which
// returns localized descriptions, genres, companies, trailers and screenshots.
type Details struct{ store *Store }

var _ media.MetadataProvider = (*Details)(nil)

// NewDetails builds the metadata provider on top of the store client.
func NewDetails(s *Store) *Details { return &Details{store: s} }

// Descriptor implements media.MetadataProvider.
func (d *Details) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               DetailsProviderID,
		Kind:             provider.KindMetadata,
		Name:             "Steam",
		DescriptionKey:   "providers.steamDetails.description",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

// LinkStore implements media.StoreLinker.
func (d *Details) LinkStore() game.Store { return LinkedStore }

// Applies implements media.MetadataProvider.
func (d *Details) Applies(q media.CoverQuery) bool { return AppIDOf(q) != 0 }

type appDetails struct {
	Success bool `json:"success"`
	Data    struct {
		Name             string   `json:"name"`
		ShortDescription string   `json:"short_description"`
		AboutTheGame     string   `json:"about_the_game"`
		Developers       []string `json:"developers"`
		Publishers       []string `json:"publishers"`
		Website          string   `json:"website"`
		RequiredAge      any      `json:"required_age"` // number or string depending on the app
		Genres           []struct {
			Description string `json:"description"`
		} `json:"genres"`
		ReleaseDate struct {
			Date string `json:"date"`
		} `json:"release_date"`
		Metacritic struct {
			Score int    `json:"score"`
			URL   string `json:"url"`
		} `json:"metacritic"`
		Ratings map[string]json.RawMessage `json:"ratings"` // shape varies by rating board: parsed leniently
		Movies  []struct {
			Name      string `json:"name"`
			Thumbnail string `json:"thumbnail"`
			HLS       string `json:"hls_h264"`
			Highlight bool   `json:"highlight"`
		} `json:"movies"`
		Screenshots []struct {
			Thumb string `json:"path_thumbnail"`
			Full  string `json:"path_full"`
		} `json:"screenshots"`
		Categories []struct {
			Description string `json:"description"`
		} `json:"categories"`
	} `json:"data"`
}

const maxVideos, maxScreenshots = 4, 12

// fetch asks the store for one app. ok is false when the store answers but has no data for it.
func (d *Details) fetch(ctx context.Context, appID int64, lang string) (app appDetails, ok bool, err error) {
	id := strconv.FormatInt(appID, 10)
	params := url.Values{"appids": {id}, "l": {lang}}

	var out map[string]appDetails

	err = d.store.Site.Get(ctx, "/api/appdetails", params, &out)
	if apiclient.IsStatus(err, http.StatusTooManyRequests) {
		return appDetails{}, false, fmt.Errorf("steam store: too many requests, try again in a few minutes")
	}

	if err != nil {
		return appDetails{}, false, fmt.Errorf("steam store: %w", err)
	}

	app, ok = out[id]

	return app, ok && app.Success, nil
}

// Test implements media.Provider: it asks the store for Portal 2 (app 620), so only transport
// errors, rate limits and HTTP errors fail it; a store with no data for the app still works.
func (d *Details) Test(ctx context.Context, _ schema.Settings) error {
	if _, _, err := d.fetch(ctx, 620, "english"); err != nil {
		return fmt.Errorf("could not reach the Steam store, check the connection and try again: %w", err)
	}

	return nil
}

// Details implements media.MetadataProvider.
func (d *Details) Details(ctx context.Context, q media.CoverQuery, language string, _ schema.Settings) (*media.GameDetails, error) {
	lang := steamLanguages[language]
	if lang == "" {
		lang = "english"
	}

	appID := AppIDOf(q)
	if appID == 0 {
		return nil, nil
	}

	app, ok, err := d.fetch(ctx, appID, lang)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, nil // removed from the store or region-locked
	}

	a := app.Data

	det := &media.GameDetails{
		Summary:       HTMLToText(a.AboutTheGame),
		Developers:    a.Developers,
		Publishers:    a.Publishers,
		ReleaseDate:   a.ReleaseDate.Date,
		Metacritic:    a.Metacritic.Score,
		MetacriticURL: a.Metacritic.URL,
		Website:       a.Website,
		StoreURL:      StorePageURL + strconv.FormatInt(appID, 10),
	}
	if det.Summary == "" {
		det.Summary = HTMLToText(a.ShortDescription)
	}

	for _, g := range a.Genres {
		det.Genres = append(det.Genres, g.Description)
	}

	for _, board := range []string{"pegi", "esrb", "usk"} {
		var r struct {
			Rating string `json:"rating"`
		}
		if raw, ok := a.Ratings[board]; ok && json.Unmarshal(raw, &r) == nil && r.Rating != "" {
			det.AgeRating = strings.ToUpper(board) + " " + strings.ToUpper(r.Rating)
			break
		}
	}
	// Highlighted trailers first.
	for _, pass := range []bool{true, false} {
		for _, m := range a.Movies {
			if m.Highlight == pass && m.HLS != "" && len(det.Videos) < maxVideos {
				det.Videos = append(det.Videos, media.Video{
					Title:     m.Name,
					Thumbnail: m.Thumbnail,
					HLSURL:    m.HLS,
				})
			}
		}
	}

	for _, s := range a.Screenshots[:min(len(a.Screenshots), maxScreenshots)] {
		det.Screenshots = append(det.Screenshots, media.Screenshot{
			ThumbURL: s.Thumb,
			FullURL:  s.Full,
		})
	}

	return det, nil
}

var (
	reBlockEnd = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/h[1-6]|/li|/ul|/div)\s*>`)
	reListItem = regexp.MustCompile(`(?i)<\s*li[^>]*>`)
	reTags     = regexp.MustCompile(`<[^>]+>`)
	reBlank    = regexp.MustCompile(`\n{3,}`)
	reSpaces   = regexp.MustCompile(`[ \t\r\f]+`)
)

// HTMLToText turns the store's HTML descriptions into plain text with paragraphs, so the UI never
// renders third-party HTML.
func HTMLToText(s string) string {
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reListItem.ReplaceAllString(s, "\n• ")
	s = reTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}

	s = strings.Join(lines, "\n")
	s = reBlank.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}
