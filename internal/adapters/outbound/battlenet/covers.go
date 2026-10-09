package battlenet

import (
	"context"
	"fmt"
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
	Search media.LinkSearcher  // the Steam store search
	Steam  media.CoverProvider // Steam's library art, for games linked to Steam
}

var _ media.CoverProvider = (*Covers)(nil)

// NewCovers returns the Battle.net cover provider with its production endpoints.
func NewCovers(search media.LinkSearcher, steam media.CoverProvider) *Covers {
	return &Covers{
		Search: search,
		Steam:  steam,
	}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "Battle.net",
		DescriptionKey:   "providers.battlenetCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     70,
	}
}

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// Applies reports whether the game is linked to Battle.net.
func (c *Covers) Applies(q media.CoverQuery) bool {
	return q.Links[LinkedStore.Key] != ""
}

// reYear drops a year Battle.net adds to tell remasters apart: "… Remastered (2017)".
var reYear = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.CoverQuery, settings schema.Settings) ([]media.CoverCandidate, error) {
	title := strings.TrimSpace(reYear.ReplaceAllString(q.Title, ""))

	matches, err := c.Search.SearchLinks(ctx, title)
	if err != nil {
		return nil, err
	}

	store := c.Search.LinkStore().Key
	key := game.MatchKey(title)

	for _, m := range matches {
		if game.MatchKey(m.Name) != key {
			continue
		}

		cands, err := c.Steam.Covers(ctx, media.CoverQuery{
			Title: m.Name,
			Links: game.Links{store: m.ID},
		}, settings)
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

// Test implements media.Provider: it searches the Steam store for one well-known title, since that
// search is the only service this provider depends on, and fails if the search errors. No match is not a failure.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := c.Search.SearchLinks(ctx, "Portal"); err != nil {
		return fmt.Errorf("the Steam store search is not answering (%w); try again later", err)
	}

	return nil
}
