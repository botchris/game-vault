package eaapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// CoverProviderID identifies the EA cover provider.
const CoverProviderID provider.ID = "ea-covers"

// Covers is a cover provider for games imported from an EA library: the official pack art from
// EA's catalog, which the EA app API serves without sign-in. Games are looked up by their slug,
// guessed from the title and the product id ("en-us_battlefield-1-standard-edition-…").
type Covers struct {
	// API calls EA's GraphQL endpoint (its BaseURL is the endpoint itself); tests point it at a
	// fake server.
	API *apiclient.Client
}

var (
	_ media.CoverProvider = (*Covers)(nil)
	_ media.ImageHoster   = (*Covers)(nil)
)

// NewCovers returns the EA app cover provider with its production endpoints.
func NewCovers() *Covers {
	api := apiclient.New(defaultGraphQLURL)
	api.HTTP.Timeout = 20 * time.Second
	api.UserAgent = apiclient.BrowserUserAgent
	api.MaxBody = 4 << 20

	return &Covers{API: api}
}

// Descriptor implements media.CoverProvider.
func (c *Covers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               CoverProviderID,
		Kind:             provider.KindCover,
		Name:             "EA",
		DescriptionKey:   "providers.eaCovers.description",
		EnabledByDefault: true,
		DefaultOrder:     60,
	}
}

// ImageHosts implements media.ImageHoster.
func (c *Covers) ImageHosts() []string { return []string{"app-images.ea.com"} }

// LinkStore implements media.StoreLinker.
func (c *Covers) LinkStore() game.Store { return LinkedStore }

// Applies reports whether the game is linked to the EA app, for its PC edition.
func (c *Covers) Applies(q media.GameQuery) bool { return q.ForPC() && q.Links[LinkedStore.Key] != "" }

var (
	reNonSlug = regexp.MustCompile(`[^a-z0-9]+`)
	reLocale  = regexp.MustCompile(`^[a-z]{2}-[a-z]{2}_`)
)

// slugify turns a title into an EA game slug: "Need for Speed™ Heat" → "need-for-speed-heat".
func slugify(title string) string {
	s, _, _ := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), title)
	s = strings.NewReplacer("®", "", "™", "", "©", "", "&", " and ", "'", "", "’", "").Replace(strings.ToLower(s))

	return strings.Trim(reNonSlug.ReplaceAllString(s, "-"), "-")
}

// slugCandidates lists slugs to try, most likely first: from the title, then the product id cut
// word by word ("battlefield-1-standard-edition-pc" → … → "battlefield-1").
func slugCandidates(title string, productIDs []string) []string {
	var out []string

	add := func(s string) {
		if s != "" && len(out) < 16 && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	add(slugify(title))
	add(slugify(game.StripYearSuffix(title)))

	for _, id := range productIDs {
		body, _, _ := strings.Cut(reLocale.ReplaceAllString(id, ""), "_")

		parts := strings.Split(body, "-")
		for n := len(parts); n >= 1; n-- {
			add(strings.Join(parts[:n], "-"))
		}
	}

	return out
}

type image struct {
	Path string `json:"path"`
}

type eaGame struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	PackArt *struct {
		Tall9x16 *image `json:"aspect9x16Image"`
		Tall5x7  *image `json:"aspect5x7Image"`
	} `json:"packArt"`
	KeyArt *struct {
		Square *image `json:"aspect1x1Image"`
		Wide   *image `json:"aspect16x9Image"`
	} `json:"keyArt"`
}

// Covers implements media.CoverProvider.
func (c *Covers) Covers(ctx context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	slugs := slugCandidates(q.Title, []string{q.Links[LinkedStore.Key]})
	// One request: every candidate slug as an alias; unknown slugs come back null.
	var b strings.Builder
	b.WriteString("{")

	for i, s := range slugs {
		fmt.Fprintf(&b, ` g%d: game(slug: %s) { slug title packArt { aspect9x16Image { path } aspect5x7Image { path } } keyArt { aspect1x1Image { path } aspect16x9Image { path } } }`,
			i, strconv.Quote(s))
	}

	b.WriteString(" }")

	// Unknown slugs come back null, sometimes with an error each: only an answer without any data
	// is a failure. A 429 or an outage says nothing about the game: the chain asks again later
	// instead of marking the cover missing.
	var data map[string]*eaGame

	err := c.API.GraphQL(ctx, "", apiclient.GraphQLBody{Query: b.String()}, nil, &data)

	var ge *apiclient.GraphQLError

	partial := errors.As(err, &ge) && ge.Partial
	if err != nil && !partial {
		return nil, fmt.Errorf("ea catalog: %w", apiclient.ClassifyError(ctx, err))
	}

	// Prefer the game whose title matches; otherwise the first (most likely) candidate found.
	type found struct {
		order int
		g     *eaGame
	}

	var games []found

	for alias, g := range data {
		if g == nil {
			continue
		}

		i, _ := strconv.Atoi(strings.TrimPrefix(alias, "g"))
		games = append(games, found{i, g})
	}

	sort.Slice(games, func(i, j int) bool {
		mi, mj := game.SameMatchKey(games[i].g.Title, q.Title), game.SameMatchKey(games[j].g.Title, q.Title)
		if mi != mj {
			return mi
		}

		return games[i].order < games[j].order
	})

	if len(games) == 0 {
		return nil, nil
	}

	g := games[0].g

	var cands []media.CoverCandidate

	add := func(img *image, label string) {
		if img != nil && strings.HasPrefix(img.Path, "https://") {
			cands = append(cands, media.CoverCandidate{
				URL:      img.Path,
				ThumbURL: img.Path,
				Label:    label,
				Title:    g.Title,
				Provider: CoverProviderID,
			})
		}
	}
	if g.PackArt != nil {
		add(g.PackArt.Tall5x7, "EA: pack art")
		add(g.PackArt.Tall9x16, "EA: pack art (tall)")
	}

	if g.KeyArt != nil {
		add(g.KeyArt.Square, "EA: key art (square)")
		add(g.KeyArt.Wide, "EA: key art (wide)")
	}

	return cands, nil
}

// Test implements media.Provider: it asks EA's public catalog for one well-known game by slug and
// fails on a transport error, an HTTP error or a GraphQL rejection. An unknown slug is not a failure.
func (c *Covers) Test(ctx context.Context, _ schema.Settings) error {
	var data map[string]*eaGame

	err := c.API.GraphQL(ctx, "", apiclient.GraphQLBody{Query: `{ g0: game(slug: "battlefield-1") { slug title } }`}, nil, &data)

	var (
		ge *apiclient.GraphQLError
		se *apiclient.StatusError
	)

	switch {
	case err == nil:
		return nil
	case errors.As(err, &ge):
		return fmt.Errorf("the catalog rejected the request (%s); try again later", ge.Message)
	case errors.As(err, &se):
		return fmt.Errorf("the catalog answered (%w); try again later", apiclient.ClassifyError(ctx, err))
	}

	return fmt.Errorf("the catalog is not reachable (%w); check the connection and try again", apiclient.ClassifyError(ctx, err))
}
