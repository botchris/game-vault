package valuation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

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
	// ErrNotValuable means the copy is not a physical copy with a barcode.
	ErrNotValuable = errkind.New(errkind.Precondition, "only physical copies with a barcode can be priced: add the copy's barcode first")

	// ErrNoProviders means no price provider is enabled.
	ErrNoProviders = errkind.New(errkind.Precondition, "no price provider is enabled: enable one on the Providers page")

	// errNotDue means the scheduler found the copy no longer due when its turn came.
	errNotDue = errors.New("no longer due")

	// errSkipped marks a provider not asked because it failed earlier in the scheduler's round.
	errSkipped = errors.New("skipped for this round after failing")

	// ErrChanged means the copy's barcode changed while its price was being estimated.
	ErrChanged = errkind.New(errkind.Conflict, "the copy changed while its price was being estimated: try again")
)

// Days between two estimates of a copy (uniform), and within which an undated copy is first planned.
const (
	minDays   = 20
	maxDays   = 40
	firstDays = 30
)

// providerTimeout bounds one provider's answer, so a slow source never holds the scheduler.
const providerTimeout = 20 * time.Second

// Configs is the port that keeps the providers' configuration and status (implemented by the
// providers registry).
type Configs interface {
	// Providers returns the configured providers of a kind in priority order.
	Providers(ctx context.Context, kind provider.Kind) ([]providers.View, error)

	// ReportResult records what a provider's answer says about the provider: a provider-level
	// problem (see providers.ProviderLevel) becomes its status on System → Health, an answer clears it.
	ReportResult(id provider.ID, err error)
}

// Service exposes the valuation use cases.
type Service struct {
	games   game.Repository
	tx      port.TxManager
	configs Configs
	prefs   settings.Repository
	impls   map[provider.ID]Provider
	now     port.Clock
	log     *slog.Logger

	// rhythm draws the estimates' dates and the scheduler's pauses, and waits them; tests replace
	// it (SetChance).
	rhythm unattended.Rhythm
}

// NewService builds the service with the price providers compiled in.
func NewService(games game.Repository, tx port.TxManager, configs Configs, prefs settings.Repository, now port.Clock,
	log *slog.Logger, prices ...Provider) *Service {
	impls := make(map[provider.ID]Provider, len(prices))
	for _, p := range prices {
		impls[p.Descriptor().ID] = p
	}

	return &Service{
		games:   games,
		tx:      tx,
		configs: configs,
		prefs:   prefs,
		impls:   impls,
		now:     now,
		log:     log,
	}
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// enabled returns the enabled price providers that are compiled in, in the user's order.
func (s *Service) enabled(ctx context.Context) ([]providers.View, error) {
	views, err := s.configs.Providers(ctx, provider.KindValuation)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(views, func(v providers.View) bool {
		_, ok := s.impls[v.ID()]
		return !v.Enabled() || !ok
	}), nil
}

// answer is what one provider said about a copy.
type answer struct {
	id       provider.ID
	estimate game.Estimate
	listed   bool
	err      error
}

// EstimateCopy asks every enabled price provider for the copy's barcode, keeps the latest estimate
// of each (a provider that failed keeps its previous one; one that no longer lists the product
// loses it) and plans the next valuation 20–40 days ahead. The warnings name the providers that
// failed for this product; unavailable reports that a provider failed at provider level (its
// status says why), so the copy's estimates may be incomplete. The copy is stamped as valued only
// when every provider answered. The providers are asked outside the transaction, so a slow source
// never blocks writes.
func (s *Service) EstimateCopy(ctx context.Context, gameID, copyID game.ID) (g *game.Game, warnings []string, unavailable bool, err error) {
	o, err := s.estimate(ctx, gameID, copyID, nil, false)

	return o.game, o.warnings, o.unavailable, err
}

// outcome is what one estimate of a copy did.
type outcome struct {
	game     *game.Game
	warnings []string

	// failed lists the providers that failed now.
	failed []provider.ID

	// unavailable reports that a provider failed at provider level (providers.ProviderLevel).
	unavailable bool
}

// estimate is EstimateCopy for the scheduler too: providers in skip are not asked (they failed
// earlier in the round) and count as failed; it also returns the providers that failed now. With
// onlyDue, a copy that is no longer due (priced by hand meanwhile) is left alone (errNotDue).
func (s *Service) estimate(ctx context.Context, gameID, copyID game.ID, skip map[provider.ID]bool, onlyDue bool) (outcome, error) {
	g, err := s.games.Get(ctx, gameID)
	if err != nil {
		return outcome{}, err
	}

	c, err := copyOf(g, copyID)
	if err != nil {
		return outcome{}, err
	}

	if onlyDue && c.NextValuation.After(s.now()) {
		return outcome{}, errNotDue
	}

	views, err := s.enabled(ctx)
	if err != nil {
		return outcome{}, err
	}

	if len(views) == 0 {
		return outcome{}, ErrNoProviders
	}

	answers, warnings, unavailable := s.ask(ctx, views, c.Barcode, skip)

	var failed []provider.ID

	for _, a := range answers {
		if a.err != nil {
			failed = append(failed, a.id)
		}
	}

	out := outcome{
		warnings:    warnings,
		failed:      failed,
		unavailable: unavailable,
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.games.Get(ctx, gameID)
		if err != nil {
			return err
		}

		current, err := copyOf(g, copyID)
		if err != nil {
			return err
		}

		if current.Barcode != c.Barcode {
			return ErrChanged
		}

		// "Not listed by any source" is only true when every source answered.
		set := g.SetEstimates
		if len(failed) > 0 {
			set = g.UpdateEstimates
		}

		next := s.now().Add(s.rhythm.Between(days(minDays), days(maxDays)))
		if _, err := set(copyID, merge(current.Estimates, answers), next, s.now()); err != nil {
			return err
		}

		out.game = g

		return s.games.Save(ctx, g)
	})

	return out, err
}

func copyOf(g *game.Game, id game.ID) (game.Copy, error) {
	for _, c := range g.Copies() {
		if c.ID == id {
			if !c.Valuable() {
				return c, ErrNotValuable
			}

			return c, nil
		}
	}

	return game.Copy{}, game.ErrCopyNotFound
}

// ask asks each provider not in skip for the barcode. unavailable reports that one failed at provider
// level: that is its status, not a warning.
func (s *Service) ask(ctx context.Context, views []providers.View, code game.Barcode, skip map[provider.ID]bool) (
	answers []answer, warnings []string, unavailable bool) {
	answers = make([]answer, 0, len(views))

	for _, v := range views {
		if skip[v.ID()] {
			answers = append(answers, answer{
				id:  v.ID(),
				err: errSkipped,
			})

			continue
		}

		pctx, cancel := context.WithTimeout(ctx, providerTimeout)
		e, err := s.impls[v.ID()].Estimate(pctx, v.Settings(), code)

		cancel()

		a := answer{id: v.ID()}

		// "Not listed" is an answer: the provider did reach its service.
		if errors.Is(err, ErrNotListed) {
			s.configs.ReportResult(v.ID(), nil)
		} else {
			s.configs.ReportResult(v.ID(), err)
		}

		switch {
		case errors.Is(err, ErrNotListed):
		case err != nil:
			a.err = err

			// A provider-level problem (a bot check, a rate limit, rejected keys) is the provider's
			// status, not a warning on every copy.
			if providers.ProviderLevel(err) {
				unavailable = true
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: %v", v.Descriptor.Name, err))
			}

			s.log.Warn("estimating a price", "provider", v.ID(), "barcode", code, "error", err)
		default:
			e.Provider, e.FetchedAt = string(v.ID()), s.now()
			if err := e.Validate(); err != nil {
				// An unusable answer is a failure: the copy still gets its next date.
				a.err = err
				warnings = append(warnings, fmt.Sprintf("%s: unusable price: %v", v.Descriptor.Name, err))
				s.log.Warn("unusable price", "provider", v.ID(), "barcode", code, "error", err)

				break
			}

			a.estimate, a.listed = e, true
		}

		answers = append(answers, a)
	}

	return answers, warnings, unavailable
}

// merge builds a copy's new estimates: one per enabled provider that listed the product, the
// previous one for a provider that failed. Providers no longer enabled drop out.
func merge(current []game.Estimate, answers []answer) []game.Estimate {
	var out []game.Estimate

	for _, a := range answers {
		switch {
		case a.err != nil:
			if i := slices.IndexFunc(current, func(e game.Estimate) bool { return e.Provider == string(a.id) }); i >= 0 {
				out = append(out, current[i])
			}
		case a.listed:
			out = append(out, a.estimate)
		}
	}

	return out
}

// ProviderTotal is one source's view of the collection's value, in minor units of the currency.
type ProviderTotal struct {
	Provider  provider.ID
	Name      string
	Sell      int64
	BuyCash   int64
	BuyCredit int64

	// Copies is how many copies the totals cover.
	Copies int

	// OtherCurrency is how many of this source's estimates are in another currency and left out.
	OtherCurrency int
}

// CollectionValue is the collection's value per source, in the default currency.
type CollectionValue struct {
	// Currency is the default currency; empty when none is chosen (then nothing is added up).
	Currency string
	Totals   []ProviderTotal
}

// CollectionValue adds up, per enabled source, the latest estimates in the default currency. Sources
// are never mixed: a shop's price and an asking price are different things.
func (s *Service) CollectionValue(ctx context.Context) (CollectionValue, error) {
	prefs, err := s.prefs.Preferences(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	views, err := s.enabled(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	out := CollectionValue{Currency: prefs.Currency}
	index := map[string]int{}

	for i, v := range views {
		index[string(v.ID())] = i
		out.Totals = append(out.Totals, ProviderTotal{
			Provider: v.ID(),
			Name:     v.Descriptor.Name,
		})
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	for _, g := range games {
		for _, c := range g.Copies() {
			for _, e := range c.Estimates {
				i, ok := index[e.Provider]
				if !ok {
					continue
				}

				t := &out.Totals[i]
				if out.Currency == "" || e.Sell.Currency != out.Currency {
					t.OtherCurrency++
					continue
				}

				t.Sell += e.Sell.Amount
				t.BuyCash += e.BuyCash.Amount
				t.BuyCredit += e.BuyCredit.Amount
				t.Copies++
			}
		}
	}

	return out, nil
}
