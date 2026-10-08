// Package media implements cover images, metadata provider configuration and store lookups.
//
// Covers are resolved through an ordered chain of cover providers (Plex-agent style) and cached in
// the config directory, so the UI never hot-links third-party sites and quotas are spent once.
package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// coverLogicChanged is when the way covers are found last improved (e.g. add-ons borrowing their
// base game's cover). "No cover found" markers older than this are ignored, so games are retried
// with the new logic instead of waiting for the marker to expire. Bump it with such changes.
var coverLogicChanged = time.Date(2026, 10, 8, 12, 15, 0, 0, time.UTC) // EA and Battle.net cover providers, fallback pass

// ErrNoCover means no image could be found for the game.
var ErrNoCover = errors.New("no cover available")

// ErrUnknownProvider means no implementation is registered under that id.
var ErrUnknownProvider = errors.New("unknown provider")

// Image is an encoded image.
type Image struct {
	Data        []byte
	ContentType string
}

// AssetStore is the port that keeps every downloaded image of a game in one place (one folder per
// game, Plex-style): its cover, the "no cover found" marker, and the images of its details sheet,
// with a record of where each one came from so it can be downloaded again if it goes missing.
type AssetStore interface {
	GetCover(id game.ID) (Image, bool, error)
	PutCover(g GameRef, img Image) error
	// MarkCoverMissing remembers that no cover was found, so the lookup is not repeated on every request.
	MarkCoverMissing(g GameRef, at time.Time) error
	CoverMissingSince(id game.ID) (time.Time, bool)
	DeleteCover(id game.ID) error

	GetAsset(id game.ID, name string) (Image, bool, error)
	PutAsset(g GameRef, name string, img Image) error
	// SetAssetSources records name → remote URL for the game's sheet images, replacing the previous
	// set; images no longer referenced are deleted.
	SetAssetSources(g GameRef, sources map[string]string) error
	AssetSource(id game.ID, name string) (string, bool)
	// DeleteAll removes everything stored for the game.
	DeleteAll(id game.ID) error
}

// ImageFetcher is the port that downloads an image from a URL.
type ImageFetcher interface {
	Fetch(ctx context.Context, url string) (Image, error)
}

// CoverQuery is what cover providers know about a game.
type CoverQuery struct {
	Title      string
	SteamAppID int64
	// PhysicalPlatforms lists the platforms of the game's physical copies ("PS3", "Xbox 360"...).
	PhysicalPlatforms []string
	// Platforms lists the platforms of every copy.
	Platforms []string
	// ExternalIDs are the ids of the copies imported from sources ("epic:…", "gog:…"), which
	// store cover providers use to find the game's own art.
	ExternalIDs []string
	// Fallback is set on the second pass, after no store had art for a game it knows: providers
	// that keep their quota for games without store art may then help too.
	Fallback bool
}

// ExternalIDsWithPrefix returns the external ids starting with prefix, without it:
// ExternalIDsWithPrefix("gog:") of ["gog:1207664643"] is ["1207664643"].
func (q CoverQuery) ExternalIDsWithPrefix(prefix string) []string {
	var out []string
	for _, id := range q.ExternalIDs {
		if rest, ok := strings.CutPrefix(id, prefix); ok && rest != "" {
			out = append(out, rest)
		}
	}
	return out
}

// HasStoreLink reports whether a digital store with its own art knows the game: a Steam AppID or
// a copy imported from Epic, GOG, Ubisoft, EA, Battle.net or Xbox. Quota-limited providers skip those games.
func (q CoverQuery) HasStoreLink() bool {
	if q.SteamAppID != 0 {
		return true
	}
	for _, store := range []string{"epic:", "gog:", "ubisoft:", "ea:", "battlenet:", "xbox:"} {
		if len(q.ExternalIDsWithPrefix(store)) > 0 {
			return true
		}
	}
	return false
}

// HasPhysical reports whether the game has at least one physical copy.
func (q CoverQuery) HasPhysical() bool { return len(q.PhysicalPlatforms) > 0 }

// QueryFor builds the cover query of a game.
func QueryFor(g *game.Game) CoverQuery {
	q := CoverQuery{Title: g.Title(), SteamAppID: g.SteamAppID()}
	for _, c := range g.Copies() {
		if c.ExternalID != "" {
			q.ExternalIDs = append(q.ExternalIDs, c.ExternalID)
		}
		if c.Platform == "" {
			continue
		}
		if c.Kind == game.KindPhysical && !slices.Contains(q.PhysicalPlatforms, c.Platform) {
			q.PhysicalPlatforms = append(q.PhysicalPlatforms, c.Platform)
		}
		if !slices.Contains(q.Platforms, c.Platform) {
			q.Platforms = append(q.Platforms, c.Platform)
		}
	}
	return q
}

// CoverCandidate is one image a provider proposes.
type CoverCandidate struct {
	URL      string
	ThumbURL string
	Label    string
	// Title is the provider's canonical title for the game the image belongs to, when it knows it.
	Title    string
	Provider provider.ID
}

// CoverProvider is the port each cover source implements (TheGamesDB, Steam...).
type CoverProvider interface {
	Descriptor() provider.Descriptor
	// Applies reports, without any network call, whether the provider can have a cover for q.
	// It keeps quota-limited providers away from games they cannot help with.
	Applies(q CoverQuery) bool
	// Covers returns candidate images, best first. No candidates is not an error.
	Covers(ctx context.Context, q CoverQuery, settings schema.Settings) ([]CoverCandidate, error)
}

// BarcodeMatch is what a barcode database knows about a product.
type BarcodeMatch struct {
	// Raw is the product name as the database has it, e.g. "Assassin's Creed Iii Ed. Special Ps3(sp)".
	Raw string
	// Title, Platform and Edition are extracted from Raw (see CleanProductTitle).
	Title    string
	Platform string
	Edition  string
	ImageURL string
	Provider provider.ID
}

// BarcodeProvider is the port each barcode database implements (UPCitemdb, EAN-Search...).
type BarcodeProvider interface {
	Descriptor() provider.Descriptor
	// Lookup returns the products registered under the code, best first. No match is not an error.
	Lookup(ctx context.Context, code game.Barcode, settings schema.Settings) ([]BarcodeMatch, error)
}

// ImageHoster is an optional port for providers whose images the browser may load through the
// image proxy. Many CDNs refuse hotlinked images, so the UI never loads them directly.
type ImageHoster interface {
	ImageHosts() []string
}

// ErrImageNotAllowed means the proxy was asked for an image outside the providers' hosts.
var ErrImageNotAllowed = errors.New("image host not allowed")

// TestResult is the outcome of a provider connection test.
type TestResult struct {
	// RemainingQuota is the provider's remaining request allowance, or -1 if it has no quota.
	RemainingQuota int
}

// Tester is an optional port for providers that can check their settings cheaply.
type Tester interface {
	Test(ctx context.Context, settings schema.Settings) (TestResult, error)
}

// AppMatch is a store search result.
type AppMatch struct {
	AppID    int64
	Name     string
	ImageURL string
}

// AppSearcher is the port that searches a store catalog by title.
type AppSearcher interface {
	SearchApps(ctx context.Context, query string) ([]AppMatch, error)
}

// Service exposes the media use cases.
type Service struct {
	games     game.Repository
	providers provider.Repository
	store     AssetStore
	fetch     ImageFetcher
	search    AppSearcher
	impls     map[provider.ID]interface{ Descriptor() provider.Descriptor }
	covers    map[provider.ID]CoverProvider
	barcodes  map[provider.ID]BarcodeProvider
	metadata  map[provider.ID]MetadataProvider
	details   DetailsStore
	order     []provider.ID // registration order = default chain order
	now       port.Clock
	log       *slog.Logger

	langs        languages
	retryMissing time.Duration // how long a "not found" result is trusted
	group        singleflight.Group
	slots        chan struct{} // limits concurrent downloads
}

// Providers groups the provider implementations available to the service. Their order is the
// default chain order the first time each provider is seen; afterwards the user's order is used.
type Providers struct {
	Covers   []CoverProvider
	Barcodes []BarcodeProvider
	Metadata []MetadataProvider
}

// NewService builds the service.
func NewService(games game.Repository, providers provider.Repository, store AssetStore, details DetailsStore, fetch ImageFetcher,
	search AppSearcher, now port.Clock, log *slog.Logger, impls Providers) *Service {
	s := &Service{
		games: games, providers: providers, store: store, details: details, fetch: fetch, search: search, now: now, log: log,
		impls:        map[provider.ID]interface{ Descriptor() provider.Descriptor }{},
		covers:       map[provider.ID]CoverProvider{},
		barcodes:     map[provider.ID]BarcodeProvider{},
		metadata:     map[provider.ID]MetadataProvider{},
		retryMissing: 7 * 24 * time.Hour, slots: make(chan struct{}, 6),
	}
	for _, p := range impls.Covers {
		id := p.Descriptor().ID
		s.covers[id], s.impls[id] = p, p
		s.order = append(s.order, id)
	}
	for _, p := range impls.Barcodes {
		id := p.Descriptor().ID
		s.barcodes[id], s.impls[id] = p, p
		s.order = append(s.order, id)
	}
	for _, p := range impls.Metadata {
		id := p.Descriptor().ID
		s.metadata[id], s.impls[id] = p, p
		s.order = append(s.order, id)
	}
	return s
}

var allKinds = []provider.Kind{provider.KindCover, provider.KindBarcode, provider.KindMetadata}

// siblings returns the stored configurations sharing a settings group, other than except.
func (s *Service) siblings(ctx context.Context, group string, except provider.ID) ([]*provider.Provider, error) {
	if group == "" {
		return nil, nil
	}
	var out []*provider.Provider
	for _, k := range allKinds {
		configs, err := s.providers.List(ctx, k)
		if err != nil {
			return nil, err
		}
		for _, c := range configs {
			if impl, ok := s.impls[c.ID()]; ok && c.ID() != except && impl.Descriptor().SettingsGroup == group {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// ProviderView is a provider's configuration together with its descriptor.
type ProviderView struct {
	*provider.Provider
	Descriptor provider.Descriptor
}

// Providers returns the configured providers of a kind in priority order, creating default
// configurations for providers seen for the first time.
func (s *Service) Providers(ctx context.Context, kind provider.Kind) ([]ProviderView, error) {
	configs, err := s.providers.List(ctx, kind)
	if err != nil {
		return nil, err
	}
	known := map[provider.ID]bool{}
	for _, c := range configs {
		known[c.ID()] = true
	}
	// Register new implementations at the end of the chain, in registration order.
	for _, id := range s.order {
		d := s.impls[id].Descriptor()
		if d.Kind != kind || known[id] {
			continue
		}
		p := provider.New(d, len(configs), s.now())
		// A new capability of an already configured service inherits its credentials, and is
		// enabled if the service is (e.g. TheGamesDB details once TheGamesDB covers have a key).
		if sib, err := s.siblings(ctx, d.SettingsGroup, id); err == nil && len(sib) > 0 {
			p.ShareSettings(sib[0].Settings(), s.now())
			if sib[0].Enabled() {
				_ = p.Configure(d, true, sib[0].Settings(), s.now()) // stays disabled if settings are incomplete
			}
		}
		if err := s.providers.Save(ctx, p); err != nil {
			return nil, err
		}
		configs = append(configs, p)
	}
	provider.Sort(configs)
	var out []ProviderView
	for _, c := range configs {
		if impl, ok := s.impls[c.ID()]; ok { // configurations of removed implementations are ignored
			out = append(out, ProviderView{c, impl.Descriptor()})
		}
	}
	return out, nil
}

func (s *Service) find(ctx context.Context, id provider.ID) (ProviderView, error) {
	impl, ok := s.impls[id]
	if !ok {
		return ProviderView{}, fmt.Errorf("%w: %q", ErrUnknownProvider, id)
	}
	list, err := s.Providers(ctx, impl.Descriptor().Kind)
	if err != nil {
		return ProviderView{}, err
	}
	for _, v := range list {
		if v.ID() == id {
			return v, nil
		}
	}
	return ProviderView{}, provider.ErrNotFound
}

// ConfigureProvider enables/disables a provider and updates its settings. Changing a cover
// provider forgets "no cover found" results, so games without a cover are looked up again.
func (s *Service) ConfigureProvider(ctx context.Context, id provider.ID, enabled bool, settings schema.Settings) (ProviderView, error) {
	v, err := s.find(ctx, id)
	if err != nil {
		return v, err
	}
	if err := v.Configure(v.Descriptor, enabled, settings, s.now()); err != nil {
		return v, err
	}
	if err := s.providers.Save(ctx, v.Provider); err != nil {
		return v, err
	}
	sib, err := s.siblings(ctx, v.Descriptor.SettingsGroup, id)
	if err != nil {
		return v, err
	}
	for _, c := range sib { // keep shared credentials in sync
		c.ShareSettings(v.Settings(), s.now())
		if err := s.providers.Save(ctx, c); err != nil {
			return v, err
		}
	}
	if v.Kind() == provider.KindCover {
		if _, err := s.RefreshCovers(ctx, true); err != nil {
			s.log.Warn("forgetting missing covers", "error", err)
		}
	}
	return v, nil
}

// ReorderProviders sets the chain order of a kind of provider (first id = tried first).
func (s *Service) ReorderProviders(ctx context.Context, kind provider.Kind, ids []provider.ID) ([]ProviderView, error) {
	views, err := s.Providers(ctx, kind)
	if err != nil {
		return nil, err
	}
	configs := make([]*provider.Provider, len(views))
	for i, v := range views {
		configs[i] = v.Provider
	}
	provider.Reorder(configs, ids, s.now())
	for _, c := range configs {
		if err := s.providers.Save(ctx, c); err != nil {
			return nil, err
		}
	}
	if kind == provider.KindCover {
		if _, err := s.RefreshCovers(ctx, true); err != nil {
			s.log.Warn("forgetting missing covers", "error", err)
		}
	}
	return s.Providers(ctx, kind)
}

// TestProvider checks a provider's settings (stored ones, overridden by those given).
func (s *Service) TestProvider(ctx context.Context, id provider.ID, settings schema.Settings) (TestResult, error) {
	v, err := s.find(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	merged := v.Settings()
	maps.Copy(merged, v.Descriptor.Fields.Merge(v.Settings(), settings))
	if err := v.Descriptor.Fields.Validate(merged, v.Descriptor.Name); err != nil {
		return TestResult{}, err
	}
	if t, ok := s.impls[id].(Tester); ok {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return t.Test(ctx, merged)
	}
	return TestResult{RemainingQuota: -1}, nil
}

// chain returns the enabled cover providers in priority order.
func (s *Service) chain(ctx context.Context) ([]ProviderView, error) {
	return s.enabled(ctx, provider.KindCover)
}

// enabled returns the enabled providers of a kind in priority order.
func (s *Service) enabled(ctx context.Context, kind provider.Kind) ([]ProviderView, error) {
	views, err := s.Providers(ctx, kind)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(views, func(v ProviderView) bool { return !v.Enabled() }), nil
}

// Cover returns the game's cover, resolving and caching it on first use.
func (s *Service) Cover(ctx context.Context, id game.ID) (Image, error) {
	if img, ok, err := s.store.GetCover(id); err != nil || ok {
		return img, err
	}
	if since, ok := s.store.CoverMissingSince(id); ok && since.After(coverLogicChanged) && s.now().Sub(since) < s.retryMissing {
		return Image{}, ErrNoCover
	}
	// Concurrent requests for the same cover share one lookup.
	v, err, _ := s.group.Do(string(id), func() (any, error) { return s.resolve(context.WithoutCancel(ctx), id) })
	if err != nil {
		return Image{}, err
	}
	return v.(Image), nil
}

func (s *Service) resolve(ctx context.Context, id game.ID) (Image, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}
	ref := GameRef{g.ID(), g.Title()}

	// A cover the user picked or pasted always wins.
	if u := g.CoverURL(); u != "" {
		s.slots <- struct{}{}
		img, err := s.fetchAndCache(ctx, ref, u)
		<-s.slots
		if err == nil {
			return img, nil
		}
		s.log.Warn("custom cover failed, falling back to providers", "game", g.Title(), "url", u)
	}

	q := QueryFor(g)
	asked := map[provider.ID]bool{}
	img, err := s.coverFromChain(ctx, ref, q, asked)
	if errors.Is(err, ErrNoCover) && q.HasStoreLink() {
		// No store had art for it (a game only on Battle.net, an old EA title…): ask the providers
		// that kept out of it, so quota-limited ones help too.
		q.Fallback = true
		img, err = s.coverFromChain(ctx, ref, q, asked)
	}
	if err == nil {
		return img, nil
	}
	if !errors.Is(err, ErrNoCover) {
		return Image{}, err
	}
	// Add-ons (DLC, soundtracks…) rarely have art of their own: borrow the base game's.
	if img, ok := s.addOnCover(ctx, g); ok {
		return img, nil
	}
	if len(asked) > 0 {
		s.log.Info("no cover found", "game", g.Title())
		if err := s.store.MarkCoverMissing(ref, s.now()); err != nil {
			s.log.Warn("marking missing cover", "error", err)
		}
	}
	return Image{}, ErrNoCover
}

// coverFromChain asks the enabled cover providers that apply to q, in order, and caches the first
// image that downloads. Providers already in asked are skipped; the ones asked are added to it.
func (s *Service) coverFromChain(ctx context.Context, ref GameRef, q CoverQuery, asked map[provider.ID]bool) (Image, error) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	chain, err := s.chain(ctx)
	if err != nil {
		return Image{}, err
	}
	for _, p := range chain {
		impl := s.covers[p.ID()]
		if asked[p.ID()] || !impl.Applies(q) {
			continue
		}
		asked[p.ID()] = true
		candidates, err := impl.Covers(ctx, q, p.Settings())
		if err != nil {
			s.log.Warn("cover provider failed", "provider", p.ID(), "game", ref.Title, "error", err)
			continue
		}
		for _, c := range candidates[:min(len(candidates), 2)] {
			if img, err := s.fetchAndCache(ctx, ref, c.URL); err == nil {
				s.log.Debug("cover cached", "game", ref.Title, "provider", p.ID(), "url", c.URL)
				return img, nil
			}
		}
	}
	return Image{}, ErrNoCover
}

func (s *Service) fetchAndCache(ctx context.Context, g GameRef, url string) (Image, error) {
	img, err := s.fetch.Fetch(ctx, url)
	if err != nil {
		return Image{}, err
	}
	if err := s.store.PutCover(g, img); err != nil {
		return Image{}, fmt.Errorf("caching cover: %w", err)
	}
	return img, nil
}

// CoverCandidates asks every enabled, applicable provider for images, for the "choose cover"
// dialog. Failing providers are reported as warnings.
func (s *Service) CoverCandidates(ctx context.Context, id game.ID) ([]CoverCandidate, []string, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	chain, err := s.chain(ctx)
	if err != nil {
		return nil, nil, err
	}
	q := QueryFor(g)
	var out []CoverCandidate
	var warnings []string
	for _, p := range chain {
		impl := s.covers[p.ID()]
		if !impl.Applies(q) {
			continue
		}
		c, err := impl.Covers(ctx, q, p.Settings())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			continue
		}
		out = append(out, c...)
	}
	return out, warnings, nil
}

// RefreshCovers drops cached covers so they are resolved again on next view. With missingOnly,
// only "not found" markers are dropped (cheap: real covers stay cached). Returns games affected.
func (s *Service) RefreshCovers(ctx context.Context, missingOnly bool) (int, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, g := range games {
		_, missing := s.store.CoverMissingSince(g.ID())
		if missingOnly && !missing {
			continue
		}
		if err := s.store.DeleteCover(g.ID()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Invalidate drops the game's cover, details and downloaded images (implements catalog.CoverCache):
// they depend on the custom cover URL and the Steam AppID, and must go when the game is deleted.
func (s *Service) Invalidate(ctx context.Context, id game.ID) error {
	if s.details != nil {
		if err := s.details.Delete(ctx, id); err != nil {
			return err
		}
	}
	return s.store.DeleteAll(id)
}

// SearchSteamApps searches the Steam store by title.
func (s *Service) SearchSteamApps(ctx context.Context, query string) ([]AppMatch, error) {
	if len(query) < 2 {
		return nil, nil
	}
	return s.search.SearchApps(ctx, query)
}

// Suggestion is a canonical game proposed for a title, with its cover.
type Suggestion struct {
	Title    string
	Platform string
	CoverURL string
	ThumbURL string
	Label    string
	Provider provider.ID
}

// GameRef points to a game already in the catalog.
type GameRef struct {
	ID    game.ID
	Title string
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
}

// IdentifyBarcode looks a scanned code up: first in the catalog, then through the barcode
// provider chain, and finally asks the cover providers for the canonical game and its cover.
func (s *Service) IdentifyBarcode(ctx context.Context, raw string) (BarcodeResult, error) {
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
	for _, g := range games {
		if c, ok := g.CopyWithBarcode(code); ok {
			res.Owned = append(res.Owned, OwnedCopy{Game: GameRef{g.ID(), g.Title()}, CopyID: c.ID, Platform: c.Platform})
		}
	}
	if len(res.Owned) > 0 {
		return res, nil // known locally: no external request
	}

	chain, err := s.enabled(ctx, provider.KindBarcode)
	if err != nil {
		return res, err
	}
	for _, p := range chain {
		matches, err := s.barcodes[p.ID()].Lookup(ctx, code, p.Settings())
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			continue
		}
		if len(matches) > 0 {
			m := matches[0]
			res.Match = &m
			break
		}
	}
	if res.Match == nil {
		return res, nil
	}
	sugg, existing, warnings, err := s.suggest(ctx, games, res.Match.Title, res.Match.Platform)
	res.Suggestions, res.Existing = sugg, existing
	res.Warnings = append(res.Warnings, warnings...)
	return res, err
}

// SuggestGames proposes canonical games for a title typed by the user (e.g. when the barcode is
// unknown), on the given platform, plus catalog games with the same title.
func (s *Service) SuggestGames(ctx context.Context, title, platform string) ([]Suggestion, []GameRef, []string, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return s.suggest(ctx, games, title, platform)
}

func (s *Service) suggest(ctx context.Context, games []*game.Game, title, platform string) ([]Suggestion, []GameRef, []string, error) {
	title, platform = strings.TrimSpace(title), game.CanonicalPlatform(platform)
	if title == "" {
		return nil, nil, nil, nil
	}
	q := CoverQuery{Title: title}
	if platform != "" {
		q.PhysicalPlatforms, q.Platforms = []string{platform}, []string{platform}
	}
	chain, err := s.chain(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	var out []Suggestion
	var warnings []string
	for _, p := range chain {
		impl := s.covers[p.ID()]
		if !impl.Applies(q) {
			continue
		}
		candidates, err := impl.Covers(ctx, q, p.Settings())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			continue
		}
		for _, c := range candidates {
			t := c.Title
			if t == "" {
				t = title
			}
			out = append(out, Suggestion{Title: t, Platform: platform, CoverURL: c.URL, ThumbURL: c.ThumbURL, Label: c.Label, Provider: c.Provider})
		}
	}

	keys := map[string]bool{game.MatchKey(title): true}
	if len(out) > 0 {
		keys[game.MatchKey(out[0].Title)] = true
	}
	var existing []GameRef
	for _, g := range games {
		if keys[g.MatchKey()] {
			existing = append(existing, GameRef{g.ID(), g.Title()})
		}
	}
	return out, existing, warnings, nil
}

// ProxyImage downloads an image for the browser. Only https URLs on hosts declared by a provider
// (ImageHoster) are allowed, so the proxy cannot be used to reach arbitrary or internal addresses.
func (s *Service) ProxyImage(ctx context.Context, rawURL string) (Image, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !s.imageHostAllowed(u.Hostname()) {
		return Image{}, ErrImageNotAllowed
	}
	return s.fetch.Fetch(ctx, u.String())
}

func (s *Service) imageHostAllowed(host string) bool {
	for _, impl := range s.impls {
		if h, ok := impl.(ImageHoster); ok && slices.Contains(h.ImageHosts(), strings.ToLower(host)) {
			return true
		}
	}
	return false
}
