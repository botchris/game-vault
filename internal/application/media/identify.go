package media

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// Suggestion is a canonical game proposed for a title, with its cover.
type Suggestion struct {
	Title    string
	Platform string
	CoverURL string
	ThumbURL string
	Label    string
	Provider provider.ID
}

// OwnedCopy is a copy already registered with a scanned barcode.
type OwnedCopy struct {
	Game     GameRef
	CopyID   game.ID
	Platform string
}

// BarcodeResult is everything known about a scanned barcode, for the user to confirm.
type BarcodeResult struct {
	Code game.Barcode

	// Owned lists copies that already carry this barcode: scanning a game you registered before.
	Owned []OwnedCopy

	// Match is the barcode databases' answer; nil when no provider knows the code.
	Match *BarcodeMatch

	// Suggestions are canonical games (with covers) for Match's title and platform.
	Suggestions []Suggestion

	// Existing lists catalog games with the same title, to add the disc as one more copy.
	Existing []GameRef
	Warnings []string

	// ProvidersUnavailable reports that a provider had a provider-level problem (see
	// providers.Registry.Statuses), so some suggestions may be missing.
	ProvidersUnavailable bool
}

// Identifier tells which game a scanned barcode or a typed title is: from the catalog first, then
// through the barcode chain, and the cover chain for the canonical game and its cover.
type Identifier struct {
	games    game.Repository
	reg      *providers.Registry
	barcodes map[provider.ID]BarcodeProvider
	covers   coverChain
	log      *slog.Logger
}

// NewIdentifier builds the identification over the barcode and cover providers in reg. Options:
// WithLogger.
func NewIdentifier(games game.Repository, reg *providers.Registry, opts ...Option) *Identifier {
	o := newOptions(opts)

	return &Identifier{
		games:    games,
		reg:      reg,
		barcodes: implsOf[BarcodeProvider](reg, provider.KindBarcode),
		covers:   newCoverChain(reg),
		log:      o.log,
	}
}

// IdentifyBarcode looks a scanned code up: first in the catalog, then through the barcode
// provider chain, and finally asks the cover providers for the canonical game and its cover.
func (s *Identifier) IdentifyBarcode(ctx context.Context, raw string) (BarcodeResult, error) {
	code, err := game.ParseBarcode(raw)
	if err != nil {
		return BarcodeResult{}, err
	}

	if code == "" {
		return BarcodeResult{}, fmt.Errorf("%w: empty barcode", game.ErrInvalidBarcode)
	}

	res := BarcodeResult{Code: code}

	games, err := s.games.List(ctx)
	if err != nil {
		return res, err
	}

	res.Owned = ownedWith(games, code)
	if len(res.Owned) > 0 {
		return res, nil // known locally: no external request
	}

	if err := s.lookUp(ctx, code, &res); err != nil {
		return res, err
	}

	if res.Match == nil {
		return res, nil
	}

	// The lookup is automatic and keeps the first proposal, so providers are not asked to browse.
	sugg, err := s.suggest(ctx, games, res.Match.Title, res.Match.Platform, false)
	res.Suggestions, res.Existing = sugg.Suggestions, sugg.Existing
	res.Warnings = append(res.Warnings, sugg.Warnings...)
	res.ProvidersUnavailable = res.ProvidersUnavailable || sugg.ProvidersUnavailable

	return res, err
}

// ownedWith returns the copies in games that carry the barcode.
func ownedWith(games []*game.Game, code game.Barcode) []OwnedCopy {
	var out []OwnedCopy

	for _, g := range games {
		if c, ok := g.CopyWithBarcode(code); ok {
			out = append(out, OwnedCopy{
				Game:     GameRef{g.ID(), g.Title()},
				CopyID:   c.ID,
				Platform: c.Platform,
			})
		}
	}

	return out
}

// lookUp asks the barcode chain for the code until a provider knows it, and records in res its
// match, the failures and whether a provider had a provider-level problem.
func (s *Identifier) lookUp(ctx context.Context, code game.Barcode, res *BarcodeResult) error {
	chain, err := s.reg.Enabled(ctx, provider.KindBarcode)
	if err != nil {
		return err
	}

	for _, p := range withImpl(chain, s.barcodes) {
		settings := p.Settings()
		matches, err := s.barcodes[p.ID()].Lookup(ctx, code, settings)
		s.reg.ReportResult(p.ID(), err)

		// A provider may have learned something worth keeping (state fields), even when it failed.
		if serr := s.reg.SaveState(ctx, p.ID(), settings); serr != nil {
			s.log.Warn("saving provider state", "provider", p.ID(), "error", serr)
		}

		switch {
		case providers.ProviderLevel(err):
			// The provider's status, like the suggestions' in the same answer: not a warning.
			res.ProvidersUnavailable = true
		case err != nil:
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
		case len(matches) > 0:
			m := matches[0]
			res.Match = &m

			return nil
		}
	}

	return nil
}

// Suggestions is what SuggestGames proposes for a title.
type Suggestions struct {
	// Suggestions are canonical games (with covers) for the title on the platform.
	Suggestions []Suggestion

	// Existing lists catalog games with the same title.
	Existing []GameRef

	// Warnings name the providers that failed for this title.
	Warnings []string

	// ProvidersUnavailable reports that a provider had a provider-level problem (see
	// providers.Registry.Statuses), so some suggestions may be missing.
	ProvidersUnavailable bool
}

// SuggestGames proposes canonical games for a title typed by the user (e.g. when the barcode is
// unknown), on the given platform, plus catalog games with the same title.
func (s *Identifier) SuggestGames(ctx context.Context, title, platform string) (Suggestions, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return Suggestions{}, err
	}

	// The user picks from the list, so providers may also propose near matches.
	return s.suggest(ctx, games, title, platform, true)
}

// suggest asks the cover providers for canonical games with the title on the platform. Browse
// is set when the user picks from the proposals (GameQuery.Browse).
func (s *Identifier) suggest(ctx context.Context, games []*game.Game, title, platform string, browse bool) (Suggestions, error) {
	title, platform = strings.TrimSpace(title), game.CanonicalPlatform(platform)
	if title == "" {
		return Suggestions{}, nil
	}

	q := GameQuery{
		Title:  title,
		Browse: browse,
	}
	if platform != "" {
		q.PhysicalPlatforms, q.Platforms = []string{platform}, []string{platform}
		q.System = game.SystemOf(platform)
	}

	chain, err := s.covers.enabled(ctx)
	if err != nil {
		return Suggestions{}, err
	}

	var res Suggestions

	for _, p := range chain {
		if !s.covers.impls[p.ID()].Applies(q) {
			continue
		}

		// Asked like the cover picker asks, so a provider-level problem is the provider's status.
		candidates, w, limited := s.covers.candidates(ctx, p, q)
		res.Warnings = append(res.Warnings, w...)
		res.ProvidersUnavailable = res.ProvidersUnavailable || limited

		for _, c := range candidates {
			t := c.Title
			if t == "" {
				t = title
			}

			res.Suggestions = append(res.Suggestions, Suggestion{
				Title:    t,
				Platform: platform,
				CoverURL: c.URL,
				ThumbURL: c.ThumbURL,
				Label:    c.Label,
				Provider: c.Provider,
			})
		}
	}

	res.Existing = sameTitle(games, title, res.Suggestions)

	return res, nil
}

// sameTitle returns the catalog games with the title, or with the first suggestion's canonical one.
func sameTitle(games []*game.Game, title string, suggestions []Suggestion) []GameRef {
	keys := map[string]bool{game.MatchKey(title): true}
	if len(suggestions) > 0 {
		keys[game.MatchKey(suggestions[0].Title)] = true
	}

	var out []GameRef

	for _, g := range games {
		if keys[g.MatchKey()] {
			out = append(out, GameRef{g.ID(), g.Title()})
		}
	}

	return out
}
