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

	"gamevault/internal/application/media"
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
		ID: DetailsProviderID, Kind: provider.KindMetadata, Name: "Steam",
		DescriptionKey: "providers.steamDetails.description", EnabledByDefault: true,
	}
}

// Applies implements media.MetadataProvider.
func (d *Details) Applies(q media.CoverQuery) bool { return q.SteamAppID != 0 }

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

// Details implements media.MetadataProvider.
func (d *Details) Details(ctx context.Context, q media.CoverQuery, language string, _ schema.Settings) (*media.GameDetails, error) {
	lang := steamLanguages[language]
	if lang == "" {
		lang = "english"
	}

	params := url.Values{"appids": {strconv.FormatInt(q.SteamAppID, 10)}, "l": {lang}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.store.StoreURL+"/api/appdetails?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	res, err := d.store.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("steam store: too many requests, try again in a few minutes")
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam store: HTTP %d", res.StatusCode)
	}

	var out map[string]appDetails
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}

	app, ok := out[strconv.FormatInt(q.SteamAppID, 10)]
	if !ok || !app.Success {
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
		StoreURL:      fmt.Sprintf("https://store.steampowered.com/app/%d", q.SteamAppID),
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
				det.Videos = append(det.Videos, media.Video{Title: m.Name, Thumbnail: m.Thumbnail, HLSURL: m.HLS})
			}
		}
	}

	for _, s := range a.Screenshots[:min(len(a.Screenshots), maxScreenshots)] {
		det.Screenshots = append(det.Screenshots, media.Screenshot{ThumbURL: s.Thumb, FullURL: s.Full})
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
