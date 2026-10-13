package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// detailsTTL is how long cached details are used before asking the providers again.
const detailsTTL = 30 * 24 * time.Hour

// detailsLogicVersion is the version of the way sheets are gathered, stored in every sheet cached
// (GameDetails.LogicVersion). A sparse sheet cached by an older version (0: before the fallback pass)
// was built without the improvement, so it is fetched again once when opened instead of waiting for
// detailsTTL; a server still running the old code may cache such sheets after the upgrade's date, so
// a version is used rather than a time. Increment it when sheet gathering improves.
const detailsLogicVersion = 3 // 1: the fallback pass; 2: Wikipedia and IGDB; 3: searches by the game's name

// Details gathers the games' sheets from the metadata chain, caches them per language, and fetches
// the missing ones in the background (RunScanner).
type Details struct {
	games     game.Repository
	reg       *providers.Registry
	metadata  map[provider.ID]MetadataProvider
	store     DetailsStore
	assets    *Assets
	autoLinks linkQueue
	now       port.Clock
	log       *slog.Logger
	group     singleflight.Group

	// langs are the UI languages the catalog is browsed in, which the background scan fetches.
	langs languages

	// rhythm varies and waits the background scan's pauses.
	rhythm unattended.Rhythm
}

// NewDetails builds the sheets service over the metadata providers in reg, caching the sheets in
// store and their images in assets. Options: WithRhythm (the background scan's pauses),
// WithClock, WithLogger. Built alone, it queues no game for automatic links: New wires the
// auto-linker.
func NewDetails(games game.Repository, reg *providers.Registry, store DetailsStore, assets *Assets, opts ...DetailsOption) *Details {
	d := &Details{
		games:     games,
		reg:       reg,
		metadata:  implsOf[MetadataProvider](reg, provider.KindMetadata),
		store:     store,
		assets:    assets,
		autoLinks: noAutoLinks{},
		now:       defaultClock,
		log:       defaultLogger,
	}
	for _, opt := range opts {
		opt.applyDetails(d)
	}

	return d
}

// GameDetails returns the game's details in the given language: from the cache when fresh,
// otherwise merged from every enabled, applicable metadata provider. refresh skips the cache.
func (s *Details) GameDetails(ctx context.Context, id game.ID, language string, refresh bool) (GameDetails, []string, error) {
	if language == "" {
		language = defaultLanguage
	}

	g, err := s.games.Get(ctx, id)
	if err != nil {
		return GameDetails{}, nil, err
	}

	ref := GameRef{g.ID(), g.Title()}

	if !refresh {
		if d, ok, err := s.store.Get(ctx, id, language); err != nil {
			return d, nil, err
		} else if ok && s.fresh(d) {
			s.assets.register(ctx, ref, &d)
			return d, nil, nil
		}
	}

	type out struct {
		d        GameDetails
		warnings []string
	}

	v, err, _ := s.group.Do("details:"+string(id)+":"+language, func() (any, error) {
		d, w, err := s.fetchDetails(context.WithoutCancel(ctx), id, language, true)
		return out{d, w}, err
	})
	if err != nil {
		return GameDetails{}, nil, err
	}

	r := v.(out)
	s.assets.register(ctx, ref, &r.d)
	s.autoLinks.Enqueue(g) // a console game may have a store sheet to find

	return r.d, r.warnings, nil
}

// fresh reports whether a cached sheet can be shown without asking the providers again: it is
// younger than detailsTTL, and not a sparse sheet gathered by an older detailsLogicVersion or one
// whose fallback pass did not complete.
func (s *Details) fresh(d GameDetails) bool {
	if d.sparse() && (d.FallbackSkipped || d.LogicVersion < detailsLogicVersion) {
		return false
	}

	return s.now().Sub(d.FetchedAt) < detailsTTL
}

// detailsPass is what one pass over the metadata chain left behind.
type detailsPass struct {
	// warnings are the failures for this game shown on the sheet; provider-level problems are the
	// providers' status instead (see providers.Registry.Statuses).
	warnings []string

	// limited reports that a provider asked to slow down.
	limited bool

	// failed reports that a provider failed, whether the failure is shown (warnings) or a
	// provider-level problem (rejected settings).
	failed bool

	// asked holds the providers already asked, so a later pass does not ask them again.
	asked map[provider.ID]bool
}

// fetchDetails asks the metadata chain for a game's sheet and caches it. When the chain answered
// without errors but the sheet is sparse (see GameDetails.sparse), and fallback is set, it runs a
// second pass with GameQuery.Fallback set: quota-limited providers (TheGamesDB) that keep out of
// games a store knows may then fill the gaps, for a game only a console store knows (a PS4 library
// copy) whose store has no details provider. The background scanner passes false: it must not spend
// those providers' quota.
func (s *Details) fetchDetails(ctx context.Context, id game.ID, language string, fallback bool) (GameDetails, []string, error) {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		return GameDetails{}, nil, err
	}

	views, err := s.reg.Enabled(ctx, provider.KindMetadata)
	if err != nil {
		return GameDetails{}, nil, err
	}

	chain := withImpl(views, s.metadata)

	q := QueryFor(g, "") // a game's sheet describes it on every system
	merged := GameDetails{
		Language:     language,
		FetchedAt:    s.now(),
		LogicVersion: detailsLogicVersion,
	}
	pass := detailsPass{asked: map[provider.ID]bool{}}

	s.askChain(ctx, g, chain, q, &merged, &pass)
	// The fallback runs only after a clean first pass: when a provider failed or was busy, the sheet
	// is not final (it is not cached, or cached to be asked again), so spending quota on it now would
	// be wasted.
	completed := false

	if fallback && merged.sparse() && !pass.failed && !pass.limited {
		q.Fallback = true
		s.askChain(ctx, g, chain, q, &merged, &pass)
		completed = !pass.failed && !pass.limited
	}
	// A sparse sheet whose fallback did not complete (skipped, or a provider failed in either pass)
	// may be cached, but the next opening runs the passes again instead of keeping it for detailsTTL.
	merged.FallbackSkipped = merged.sparse() && !completed
	// Cache even an empty sheet so unknown games are not looked up on every view; failures are not
	// cached. Nor is a sheet a provider was too busy to fill: cached, it would lack that provider's
	// part until the cache expires.
	if !pass.limited && (!pass.failed || !merged.empty()) {
		if err := s.store.Put(ctx, id, merged); err != nil {
			s.log.Warn("caching details", "error", err)
		}
	}

	return merged, pass.warnings, nil
}

// askChain asks every applicable provider of the chain not asked yet, filling merged's gaps.
func (s *Details) askChain(ctx context.Context, g *game.Game, chain []providers.View, q GameQuery, merged *GameDetails, pass *detailsPass) {
	for _, p := range chain {
		impl := s.metadata[p.ID()]
		if pass.asked[p.ID()] || !impl.Applies(q) {
			continue
		}

		pass.asked[p.ID()] = true

		d, err := impl.Details(ctx, q, merged.Language, p.Settings())
		s.reg.ReportResult(p.ID(), err)

		// Provider-level problems (busy, allowance used up, rejected settings) are the provider's
		// status, not a warning on every sheet; a rate limit still keeps the sheet out of the cache.
		if errors.Is(err, provider.ErrRateLimited) {
			pass.limited = true

			s.log.Info("metadata provider unavailable, asking again later", "provider", p.ID(), "game", g.Title(), "error", err)

			continue
		}

		if err != nil {
			pass.failed = true

			if !providers.ProviderLevel(err) {
				pass.warnings = append(pass.warnings, fmt.Sprintf("%s: %v", p.Descriptor.Name, err))
			}

			s.log.Warn("metadata provider failed", "provider", p.ID(), "game", g.Title(), "error", err)

			continue
		}

		if d == nil {
			merged.NotFoundBy = append(merged.NotFoundBy, p.ID())
			continue
		}

		if merged.fill(*d) {
			merged.Sources = append(merged.Sources, p.ID())
		}
	}
}
