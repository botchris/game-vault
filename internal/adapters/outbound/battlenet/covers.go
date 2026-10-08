package battlenet

import (
	"context"
	"regexp"
	"strings"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Battle.net cover provider.
const CoverProviderID provider.ID = "battlenet-covers"

// Covers is a cover provider for games imported from a Battle.net account. Blizzard publishes no
// box art outside its signed-in shop, so it looks the game up on the Steam store by its exact
// title (many Blizzard and Activision games are sold there too) and uses Steam's library art.
// Games only on Battle.net (World of Warcraft, StarCraft II…) are left to the next providers.
type Covers struct {
	Search media.AppSearcher   // the Steam store search
	Steam  media.CoverProvider // Steam's library art, by AppID
}

var _ media.CoverProvider = (*Covers)(nil)

func NewCovers(search media.AppSearcher, steam media.CoverProvider) *Covers {
	return &Covers{Search: search, Steam: steam}
}

func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: CoverProviderID, Kind: provider.KindCover, Name: "Battle.net",
		DescriptionKey: "providers.battlenetCovers.description", EnabledByDefault: true,
	}
}

// Applies: only games with a copy imported from a Battle.net account.
func (c *Covers) Applies(q media.CoverQuery) bool {
	return len(q.ExternalIDsWithPrefix("battlenet:")) > 0
}

// reYear drops a year Battle.net adds to tell remasters apart: "… Remastered (2017)".
var reYear = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, settings schema.Settings) ([]media.CoverCandidate, error) {
	title := strings.TrimSpace(reYear.ReplaceAllString(q.Title, ""))
	apps, err := c.Search.SearchApps(ctx, title)
	if err != nil {
		return nil, err
	}
	key := game.MatchKey(title)
	for _, a := range apps {
		if game.MatchKey(a.Name) != key {
			continue
		}
		cands, err := c.Steam.Covers(ctx, media.CoverQuery{Title: a.Name, SteamAppID: a.AppID}, settings)
		if err != nil {
			return nil, err
		}
		for i := range cands {
			cands[i].Label = "Battle.net (Steam): " + cands[i].Label
			cands[i].Provider = CoverProviderID
		}
		return cands, nil
	}
	return nil, nil
}
