package battlenet

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the Battle.net cover provider.
const CoverProviderID provider.ID = "battlenet-covers"

// Covers is a cover provider for games imported from a Battle.net account. Blizzard publishes no
// box art outside its signed-in shop, so it looks the game up on the Steam store by its title (many
// Blizzard and Activision games are sold there too) and uses Steam's library art. Games only on
// Battle.net (World of Warcraft, StarCraft II…) are left to the next providers.
type Covers struct {
	// Steam finds the game on the Steam store and gives its library art.
	Steam SteamStore
}

var _ media.CoverProvider = (*Covers)(nil)

// NewCovers returns the Battle.net cover provider, which finds its art through the Steam store.
func NewCovers(steam SteamStore) *Covers {
	return &Covers{Steam: steam}
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

// Applies reports whether the game is linked to Battle.net, for its PC edition. The art is found by
// searching the title on Steam, so never when only store links may be used (q.LinksOnly).
func (c *Covers) Applies(q media.GameQuery) bool {
	return q.ForPC() && q.Links[LinkedStore.Key] != "" && !q.LinksOnly
}

// Covers implements media.CoverProvider. The game is found by Steam's own title rules (an exact
// title first, and only a single match), so Battle.net never shows another game's box that a
// looser comparison would take.
func (c *Covers) Covers(ctx context.Context, q media.GameQuery, settings schema.Settings) ([]media.CoverCandidate, error) {
	// Battle.net adds a year to tell remasters apart ("… Remastered (2017)"); Steam does not.
	title := strings.TrimSpace(game.StripYearSuffix(q.Title))

	appID, err := c.Steam.AppIDByTitle(ctx, title)
	if err != nil || appID == 0 {
		return nil, err
	}

	cands, err := c.Steam.Covers(ctx, media.GameQuery{
		Title: title,
		Links: game.Links{game.LinkSteam: strconv.FormatInt(appID, 10)},
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

// Test implements providers.Impl: it looks one well-known title up on the Steam store, since that
// search is the only service this provider depends on, and fails if the search errors. No match
// is not a failure.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	if _, err := c.Steam.AppIDByTitle(ctx, "Portal"); err != nil {
		return fmt.Errorf("the Steam store search is not answering (%w); try again later", err)
	}

	return nil
}
