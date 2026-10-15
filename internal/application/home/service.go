package home

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/ownership"
	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/settings"
)

// Errors returned by the service.
var (
	// ErrUnknownBlock means the block asked for is not deals or releases.
	ErrUnknownBlock = errkind.New(errkind.Invalid, "unknown home block: refresh deals or releases")

	// ErrTooSoon means the block was refreshed (or tried) less than 15 minutes ago.
	ErrTooSoon = errkind.New(errkind.Exhausted, "this block was refreshed less than 15 minutes ago: try again in a few minutes")

	// ErrNotConfigured means no provider of the block's kind is enabled.
	ErrNotConfigured = errkind.New(errkind.Precondition, "no provider is enabled for this block: enable one on the Providers page")
)

const (
	// dealsEvery and releasesEvery are the job's intervals (each varied ±10 %): deals change
	// within the day, release dates rarely.
	dealsEvery    = 6 * time.Hour
	releasesEvery = 24 * time.Hour

	// manualGap is the shortest time between two refreshes the user asks for, counted from the
	// last attempt, failed ones included: the services ask for restraint.
	manualGap = 15 * time.Minute

	// fetchedDeals is how many deals of each list are gathered, so enough remain once the owned
	// games are left out; shownDeals is how many the page shows. fetchedDeals must match the limit
	// of the featured request the deals providers' Test sends (40), so a Test checks the request a
	// refresh makes.
	fetchedDeals = 40
	shownDeals   = 12

	// anticipationWindow is how far ahead the most anticipated releases are looked for. It must
	// match the window of the releases providers' Test request (60 days, limit 40), so a Test checks
	// the request a refresh makes.
	anticipationWindow = 60 * 24 * time.Hour

	// refreshTimeout bounds one block's refresh, every provider of its chain included.
	refreshTimeout = 2 * time.Minute
)

// Configs is the port that keeps the providers' configuration and status (implemented by the
// providers registry).
type Configs interface {
	// Enabled returns the enabled providers of a kind in priority order.
	Enabled(ctx context.Context, kind provider.Kind) ([]providers.View, error)

	// ReportResult records what a provider's answer says about the provider: a provider-level
	// problem becomes its status on System → Health, an answer clears it.
	ReportResult(id provider.ID, err error)
}

// Catalog is the port that reads the games. A game.Repository satisfies it.
type Catalog interface {
	// List returns every game with its copies.
	List(ctx context.Context) ([]*game.Game, error)
}

// Ownership is the port that says whether the user owns what a deal is for (implemented by
// ownership.Service): the same answer the store pages get, so a deal and a store page never
// disagree.
type Ownership interface {
	// Check answers each item, in order. It takes at most ownership.MaxItems items.
	Check(ctx context.Context, items []ownership.Item) ([]ownership.Result, error)
}

// Preferences is the port that reads the user's preferences (the default currency, which gives
// the deals' country). A settings.Repository satisfies it.
type Preferences interface {
	// Preferences returns the saved preferences, empty when none were saved.
	Preferences(ctx context.Context) (settings.Preferences, error)
}

// State is what an external block can show.
type State string

// Values of State.
const (
	StateFresh         State = "fresh"          // recent data, the last attempt worked
	StateStale         State = "stale"          // the last attempt failed, or the data is old
	StateNotConfigured State = "not_configured" // no provider enabled: the stored data is hidden
	StateNeverFetched  State = "never_fetched"  // a provider is enabled, nothing gathered yet
)

// BlockView is an external block's state as the page shows it.
type BlockView struct {
	State State

	// Provider names the first enabled provider ("IsThereAnyDeal"), for the messages and the
	// credit; "" when none is.
	Provider string

	// FetchedAt is when the data shown was gathered; FailedAt, Error and ErrorKind describe the
	// last attempt when it failed.
	FetchedAt time.Time
	FailedAt  time.Time
	Error     string
	ErrorKind ErrorKind

	// RefreshableAt is when the user may refresh the block again; zero: now.
	RefreshableAt time.Time
}

// Home is the home page's external blocks, ready to show.
type Home struct {
	Deals          BlockView
	Featured       []game.Deal
	HistoricalLows []game.Deal

	Releases    BlockView
	FromSeries  []game.Release
	Anticipated []game.Release
}

// Service gathers and serves the home page's external blocks.
type Service struct {
	configs  Configs
	games    Catalog
	owners   Ownership
	snaps    Snapshots
	prefs    Preferences
	deals    map[provider.ID]DealsProvider
	releases map[provider.ID]ReleasesProvider
	now      port.Clock
	log      *slog.Logger

	// rhythm draws and waits the job's pauses (see Run).
	rhythm unattended.Rhythm

	// mu makes refreshes one at a time: the job's and the user's never overlap, and a user's
	// refresh right after the job's is refused by the guard instead of asking again.
	mu sync.Mutex
}

// NewService builds the service. Without providers (WithDeals, WithReleases) both blocks say they
// are not configured.
func NewService(configs Configs, games Catalog, owners Ownership, snaps Snapshots, prefs Preferences, opts ...Option) *Service {
	s := &Service{
		configs:  configs,
		games:    games,
		owners:   owners,
		snaps:    snaps,
		prefs:    prefs,
		deals:    map[provider.ID]DealsProvider{},
		releases: map[provider.ID]ReleasesProvider{},
		now:      time.Now,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, o := range opts {
		o(s)
	}

	return s
}

// kindOfBlock is the provider kind that feeds each block.
var kindOfBlock = map[Block]provider.Kind{
	BlockDeals:    provider.KindDeals,
	BlockReleases: provider.KindReleases,
}

// every returns the job's interval for a block.
func every(b Block) time.Duration {
	if b == BlockDeals {
		return dealsEvery
	}

	return releasesEvery
}

// lastAttempt returns when the block was last refreshed or tried; zero when never.
func lastAttempt(s Snapshot) time.Time {
	if s.FailedAt.After(s.FetchedAt) {
		return s.FailedAt
	}

	return s.FetchedAt
}

// enabled returns the block's enabled providers that are compiled in, in the user's order.
func (s *Service) enabled(ctx context.Context, b Block) ([]providers.View, error) {
	views, err := s.configs.Enabled(ctx, kindOfBlock[b])
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(views, func(v providers.View) bool {
		if b == BlockDeals {
			return s.deals[v.ID()] == nil
		}

		return s.releases[v.ID()] == nil
	}), nil
}

// Home returns the external blocks. It only reads: no provider is asked, so a server started
// with -no-unattended never reaches a service on its own. Games owned on PC are left out of the
// deals now, not when they were gathered, so a game bought since is gone at once.
func (s *Service) Home(ctx context.Context) (Home, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return Home{}, err
	}

	var out Home

	view, snap, err := s.block(ctx, BlockDeals)
	if err != nil {
		return Home{}, err
	}

	out.Deals = view
	if view.State != StateNotConfigured {
		if out.Featured, out.HistoricalLows, err = s.unowned(ctx, snap.Featured, snap.HistoricalLows); err != nil {
			return Home{}, err
		}
	}

	view, snap, err = s.block(ctx, BlockReleases)
	if err != nil {
		return Home{}, err
	}

	out.Releases = view
	if view.State != StateNotConfigured {
		today := game.DateOf(s.now())
		ids := map[game.ID]bool{}

		for _, g := range games {
			ids[g.ID()] = true
		}

		out.FromSeries = upcoming(snap.FromSeries, today, ids)
		out.Anticipated = upcoming(snap.Anticipated, today, nil)
	}

	return out, nil
}

// block returns a block's view and its stored snapshot.
func (s *Service) block(ctx context.Context, b Block) (BlockView, Snapshot, error) {
	views, err := s.enabled(ctx, b)
	if err != nil {
		return BlockView{}, Snapshot{}, err
	}

	if len(views) == 0 {
		return BlockView{State: StateNotConfigured}, Snapshot{}, nil
	}

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return BlockView{}, Snapshot{}, err
	}

	v := BlockView{
		Provider:  views[0].Descriptor.Name,
		FetchedAt: snap.FetchedAt,
		FailedAt:  snap.FailedAt,
		Error:     snap.Error,
		ErrorKind: snap.ErrorKind,
	}

	if last := lastAttempt(snap); !last.IsZero() {
		v.RefreshableAt = last.Add(manualGap)
	}

	switch {
	case snap.FetchedAt.IsZero() && snap.Error == "":
		v.State = StateNeverFetched
	case snap.Error != "" || s.now().Sub(snap.FetchedAt) > 2*every(b):
		// Old data after a long downtime is shown with its age, like data whose refresh failed.
		v.State = StateStale
	default:
		v.State = StateFresh
	}

	return v, snap, nil
}

// upcoming keeps the releases not out yet (a release day is still upcoming) and, when owned is
// given, those whose owned game is still in the catalog.
func upcoming(in []game.Release, today game.Date, owned map[game.ID]bool) []game.Release {
	out := make([]game.Release, 0, len(in))

	for _, r := range in {
		if r.Date != "" && r.Date < today {
			continue
		}

		if owned != nil && r.Because != "" && !owned[r.Because] {
			continue
		}

		out = append(out, r)
	}

	return out
}

// Refresh gathers a block now, for the user, at most once every 15 minutes (failed attempts
// count). A failure of the service is not an error: the returned home shows it on the block, with
// the last good data.
func (s *Service) Refresh(ctx context.Context, b Block) (Home, error) {
	if _, ok := kindOfBlock[b]; !ok {
		return Home{}, ErrUnknownBlock
	}

	if err := s.refreshGuarded(ctx, b); err != nil {
		return Home{}, err
	}

	return s.Home(ctx)
}

// refreshGuarded is Refresh's refresh, under the lock and the guard.
func (s *Service) refreshGuarded(ctx context.Context, b Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return err
	}

	if last := lastAttempt(snap); !last.IsZero() && s.now().Before(last.Add(manualGap)) {
		return ErrTooSoon
	}

	return s.refresh(ctx, b)
}

// refresh asks the block's providers in the user's order until one answers, and stores what it
// gave; when all fail, it stores the last failure next to the last good data. It returns
// ErrNotConfigured when no provider is enabled, and only storage errors otherwise. s.mu is held.
func (s *Service) refresh(ctx context.Context, b Block) error {
	views, err := s.enabled(ctx, b)
	if err != nil {
		return err
	}

	if len(views) == 0 {
		return ErrNotConfigured
	}

	asking, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	var last error

	for _, v := range views {
		gathered, err := s.gather(asking, b, v)
		s.configs.ReportResult(v.ID(), err)

		if err == nil {
			gathered.Block, gathered.FetchedAt = b, s.now()
			return s.snaps.SaveSnapshot(ctx, gathered)
		}

		last = err

		if asking.Err() != nil {
			break
		}
	}

	if ctx.Err() != nil {
		return ctx.Err() // shutting down: nothing failed on the service's side
	}

	s.log.Warn("refreshing the home page", "block", b, "error", last)

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return err
	}

	snap.Block, snap.Error, snap.ErrorKind, snap.FailedAt = b, last.Error(), kindOf(last), s.now()

	return s.snaps.SaveSnapshot(ctx, snap)
}

// gather asks one provider for a block's lists. Both lists must come, or neither is kept.
func (s *Service) gather(ctx context.Context, b Block, v providers.View) (Snapshot, error) {
	if b == BlockDeals {
		p := s.deals[v.ID()]
		country := s.country(ctx)

		featured, err := p.Deals(ctx, v.Settings(), DealQuery{
			List:    DealsFeatured,
			Country: country,
			Limit:   fetchedDeals,
		})
		if err != nil {
			return Snapshot{}, err
		}

		lows, err := p.Deals(ctx, v.Settings(), DealQuery{
			List:    DealsHistoricalLows,
			Country: country,
			Limit:   fetchedDeals,
		})
		if err != nil {
			return Snapshot{}, err
		}

		return Snapshot{
			Featured:       featured,
			HistoricalLows: lows,
		}, nil
	}

	p := s.releases[v.ID()]

	anticipated, err := p.Anticipated(ctx, v.Settings(), anticipationWindow)
	if err != nil {
		return Snapshot{}, err
	}

	owned, err := s.owned(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var series []game.Release

	// An empty collection has no series: the provider is not asked with an empty list.
	if len(owned) > 0 {
		if series, err = p.Sequels(ctx, v.Settings(), owned); err != nil {
			return Snapshot{}, err
		}
	}

	return Snapshot{
		FromSeries:  series,
		Anticipated: anticipated,
	}, nil
}

// owned returns the games the user still has (a held copy), as releases providers need them.
func (s *Service) owned(ctx context.Context) ([]OwnedRef, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	var out []OwnedRef

	for _, g := range games {
		if slices.ContainsFunc(g.Copies(), game.Copy.Held) {
			out = append(out, OwnedRef{
				Game:  g.ID(),
				Title: g.Title(),
				Links: g.Links(),
			})
		}
	}

	return out, nil
}

// country returns the deals' country from the preferred currency; "" lets the provider decide.
// Preferences that cannot be read only lose that hint.
func (s *Service) country(ctx context.Context) string {
	p, err := s.prefs.Preferences(ctx)
	if err != nil {
		return ""
	}

	return countryFor(p.Currency)
}

// countries maps currencies to the country whose prices to ask for. Euro-zone shops mostly charge
// one price; the euro asks for Spain's (the maintainer's choice) and anyone sets their own country in the provider.
var countries = map[string]string{
	"EUR": "ES", "USD": "US", "GBP": "GB", "CAD": "CA", "AUD": "AU", "NZD": "NZ", "JPY": "JP",
	"BRL": "BR", "MXN": "MX", "PLN": "PL", "CHF": "CH", "SEK": "SE", "NOK": "NO", "DKK": "DK",
	"CZK": "CZ", "HUF": "HU", "TRY": "TR", "INR": "IN", "KRW": "KR", "CNY": "CN", "ARS": "AR",
	"CLP": "CL", "COP": "CO", "ZAR": "ZA",
}

// countryFor returns the country of a currency, "" when there is no table entry.
func countryFor(currency string) string {
	return countries[strings.ToUpper(strings.TrimSpace(currency))]
}

// kindOf classifies a refresh's error for a message the page translates.
func kindOf(err error) ErrorKind {
	switch {
	case errors.Is(err, provider.ErrQuotaExhausted):
		return ErrorQuota
	case errors.Is(err, provider.ErrBadSettings):
		return ErrorSettings
	case errors.Is(err, provider.ErrUnavailable):
		return ErrorUnavailable
	case errors.Is(err, provider.ErrRateLimited):
		return ErrorRateLimited
	}

	return ErrorFailed
}
