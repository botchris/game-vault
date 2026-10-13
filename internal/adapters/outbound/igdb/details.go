package igdb

import (
	"context"
	"regexp"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// DetailsID identifies IGDB in the metadata chain. It shares the Twitch credentials with the cover
// provider (same settings group).
const DetailsID provider.ID = "igdb-details"

const maxScreenshots, maxVideos = 8, 4

// detailsFields are the game fields a sheet needs, expanded.
var detailsFields = []string{
	"name", "summary", "first_release_date", "url", "genres.name",
	"involved_companies.company.name", "involved_companies.developer", "involved_companies.publisher",
	"age_ratings.organization.name", "age_ratings.rating_category.rating",
	"screenshots.image_id", "videos.name", "videos.video_id",
}

// Details implements media.MetadataProvider: summary, genres, companies, release date, age rating,
// screenshots and YouTube trailers of any game IGDB knows. IGDB has no localized summaries: its
// summary is English in every language, and the chain order decides whether it shows.
type Details struct{ c *Client }

var (
	_ media.MetadataProvider = (*Details)(nil)
	_ media.ImageHoster      = (*Details)(nil)
	_ media.LinkFinder       = (*Details)(nil)
)

// NewDetails builds the metadata provider on c, shared with the cover provider.
func NewDetails(c *Client) *Details { return &Details{c: c} }

// Descriptor implements media.MetadataProvider.
func (d *Details) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             DetailsID,
		Kind:           provider.KindMetadata,
		Name:           "IGDB",
		DescriptionKey: "providers.igdbDetails.description",
		Fields:         fields(),
		SettingsGroup:  string(CoversID),
		DefaultOrder:   15, // after Steam (10) and Wikipedia (12), before TheGamesDB (20)
	}
}

// Test implements providers.Impl.
func (d *Details) Test(ctx context.Context, s schema.Settings) error { return d.c.Test(ctx, s) }

// Applies implements media.MetadataProvider: IGDB has no quota, so it is asked about every game.
func (d *Details) Applies(media.GameQuery) bool { return true }

// ImageHosts implements media.ImageHoster: screenshots and YouTube thumbnails go through the proxy.
func (d *Details) ImageHosts() []string { return []string{"images.igdb.com", "i.ytimg.com"} }

// Details implements media.MetadataProvider.
func (d *Details) Details(ctx context.Context, q media.GameQuery, _ string, s schema.Settings) (*media.GameDetails, error) {
	g, err := d.c.find(ctx, s, q, detailsFields)
	if err != nil || g == nil {
		return nil, err
	}

	det := &media.GameDetails{
		Summary:   strings.TrimSpace(g.Summary),
		AgeRating: ageRating(g.AgeRatings),
		StoreURL:  g.URL,
	}
	if g.FirstReleaseDate != 0 {
		det.ReleaseDate = time.Unix(g.FirstReleaseDate, 0).UTC().Format(time.DateOnly)
	}

	for _, genre := range g.Genres {
		det.Genres = append(det.Genres, genre.Name)
	}

	for _, ic := range g.InvolvedCompanies {
		if ic.Developer {
			det.Developers = append(det.Developers, ic.Company.Name)
		}

		if ic.Publisher {
			det.Publishers = append(det.Publishers, ic.Company.Name)
		}
	}

	for _, sc := range g.Screenshots[:min(len(g.Screenshots), maxScreenshots)] {
		det.Screenshots = append(det.Screenshots, media.Screenshot{
			ThumbURL: imageURL("screenshot_big", sc.ImageID),
			FullURL:  imageURL("1080p", sc.ImageID),
		})
	}

	for _, v := range g.Videos {
		if v.VideoID == "" || len(det.Videos) == maxVideos {
			continue
		}

		det.Videos = append(det.Videos, media.Video{
			Title:     v.Name,
			YouTubeID: v.VideoID,
			Thumbnail: "https://i.ytimg.com/vi/" + v.VideoID + "/hqdefault.jpg",
		})
	}

	return det, nil
}

// linkFields are the game fields FindLinks needs: its store ids.
var linkFields = []string{"name", "external_games.external_game_source", "external_games.uid"}

// FindLinks implements media.LinkFinder: the Steam AppID IGDB lists for the game it matched (the same
// match as the sheet's: a store link it knows, else the only game with the title). IGDB also lists
// GOG and Epic ids, but their format has not been checked against Game Vault's links with a real
// account yet (see .claude/memory/igdb.md), so they are not used.
func (d *Details) FindLinks(ctx context.Context, q media.GameQuery, s schema.Settings) (game.Links, error) {
	g, err := d.c.find(ctx, s, q, linkFields)
	if err != nil || g == nil {
		return nil, err
	}

	links := game.Links{}

	for _, e := range g.ExternalGames {
		if e.Source == steamSource && reUID.MatchString(e.UID) && links[game.LinkSteam] == "" {
			links[game.LinkSteam] = e.UID
		}
	}

	return links, nil
}

// ratingBoards are the boards whose ratings the sheet shows, in order of preference (Steam's too).
var ratingBoards = []string{"PEGI", "ESRB", "USK"}

// pegiWords are PEGI's ratings as IGDB names them.
var pegiWords = map[string]string{
	"three":    "3",
	"seven":    "7",
	"twelve":   "12",
	"sixteen":  "16",
	"eighteen": "18",
}

// esrbNames are ESRB's ratings as IGDB may name them, by their letters on the box.
var esrbNames = map[string]string{
	"e10":             "E10+",
	"everyone 10+":    "E10+",
	"everyone":        "E",
	"teen":            "T",
	"mature":          "M",
	"adults only":     "AO",
	"rating pending":  "RP",
	"early childhood": "EC",
}

var reBoardPrefix = regexp.MustCompile(`(?i)^(pegi|esrb|usk)[\s_-]*`)

// ageRating formats the preferred board's rating the way the sheet shows it ("PEGI 18", "ESRB M").
func ageRating(ratings []ageRatingJSON) string {
	for _, board := range ratingBoards {
		for _, r := range ratings {
			if !strings.EqualFold(r.Organization.Name, board) || r.Category.Rating == "" {
				continue
			}

			v := reBoardPrefix.ReplaceAllString(strings.TrimSpace(r.Category.Rating), "")

			switch board {
			case "PEGI":
				if n, ok := pegiWords[strings.ToLower(v)]; ok {
					v = n
				}
			case "ESRB":
				if n, ok := esrbNames[strings.ToLower(v)]; ok {
					v = n
				}
			}

			return board + " " + strings.ToUpper(v)
		}
	}

	return ""
}
