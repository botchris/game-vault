package igdb

import (
	"context"
	"strconv"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoversID identifies IGDB in the cover chain and names the settings group its providers share.
const CoversID provider.ID = "igdb"

// coverFields are the game fields a cover needs.
var coverFields = []string{"name", "first_release_date", "cover.image_id"}

// Covers implements media.CoverProvider. IGDB's covers belong to the game, not to a system (often
// the PC or the first release's box), so they are a last resort for any edition: the fallback pass,
// after no other provider had art for it. A game's cover on a console edition beats no box.
type Covers struct{ c *Client }

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers builds the cover provider on c, shared with the metadata provider.
func NewCovers(c *Client) *Covers { return &Covers{c: c} }

// Descriptor implements media.CoverProvider.
func (p *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             CoversID,
		Kind:           provider.KindCover,
		Name:           "IGDB",
		DescriptionKey: "providers.igdb.description",
		Fields:         fields(),
		SettingsGroup:  string(CoversID),
		// Disabled until the user enters a Twitch application, like every provider that needs
		// credentials; enabling it retries the editions marked without cover.
		// After TheGamesDB and every store (Xbox is 80): a store's own art fits its edition better.
		DefaultOrder: 85,
	}
}

// Test implements media.Provider.
func (p *Covers) Test(ctx context.Context, s schema.Settings) error { return p.c.Test(ctx, s) }

// Applies implements media.CoverProvider: only on the fallback pass, for any system. The media
// service runs that pass whenever the first one found nothing, so an edition no other provider
// applies to still gets it.
func (p *Covers) Applies(q media.CoverQuery) bool { return q.Fallback && !q.LinksOnly }

// ImageHosts implements media.ImageHoster.
func (p *Covers) ImageHosts() []string { return []string{"images.igdb.com"} }

// Covers implements media.CoverProvider.
func (p *Covers) Covers(ctx context.Context, q media.CoverQuery, s schema.Settings) ([]media.CoverCandidate, error) {
	g, err := p.c.find(ctx, s, q, coverFields)
	if err != nil || g == nil || g.Cover.ImageID == "" {
		return nil, err
	}

	label := g.Name
	if g.FirstReleaseDate != 0 {
		label += " · " + strconv.Itoa(time.Unix(g.FirstReleaseDate, 0).UTC().Year())
	}

	return []media.CoverCandidate{{
		URL:      imageURL("cover_big_2x", g.Cover.ImageID),
		ThumbURL: imageURL("cover_big", g.Cover.ImageID),
		Label:    label,
		Title:    g.Name,
		Provider: CoversID,
	}}, nil
}
