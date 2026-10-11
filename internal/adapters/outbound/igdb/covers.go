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
// the PC or the first release's box), so they fill PC editions and, on the fallback pass, editions
// no other provider had art for.
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
		// After TheGamesDB and every store (Xbox is 80): a store's own art fits its edition better.
		DefaultOrder: 85,
	}
}

// Test implements media.Provider.
func (p *Covers) Test(ctx context.Context, s schema.Settings) error { return p.c.Test(ctx, s) }

// Applies implements media.CoverProvider: a game's cover fits its PC edition (or a game without
// copies); a console edition only gets it when nothing else had art for it.
func (p *Covers) Applies(q media.CoverQuery) bool { return q.ForPC() || q.Fallback }

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
