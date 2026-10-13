// Package upgrade rewrites, once, at start-up, what older versions stored in a shape the current
// one only converts when it reads it: game documents before version 3, the security settings of
// the versions that trusted Tailscale identities, and the covers cached before editions. Once a
// database has been through it, the readers of those old shapes are no longer needed; the next
// release removes them together with this package.
//
// It is idempotent: on later starts it finds nothing, costs one query and a look at the game-data
// folders, and logs nothing.
package upgrade

import (
	"context"
	"log/slog"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/auth"
	"gamevault/internal/domain/game"
)

// batchSize is how many games are rewritten per transaction: few enough that one transaction never
// holds the only writer for long, many enough that 1,000 old games take a handful of commits.
const batchSize = 100

// Games is the port to the stored games that can be in an older document version.
type Games interface {
	// OutdatedIDs returns the ids of the games stored in an older document version, without
	// reading the documents.
	OutdatedIDs(ctx context.Context) ([]game.ID, error)

	// Get reads a game, converting an older document as it does.
	Get(ctx context.Context, id game.ID) (*game.Game, error)

	// Save writes the game in the current document version, as it is: no date or link changes.
	Save(ctx context.Context, g *game.Game) error
}

// AuthSettings is the port to the stored security settings.
type AuthSettings interface {
	// AuthOutdated reports whether the security settings are stored in an older shape.
	AuthOutdated(ctx context.Context) (bool, error)

	// Auth reads the security settings, converting an older shape as it does.
	Auth(ctx context.Context) (auth.Settings, error)

	// SaveAuth writes the security settings in the current shape.
	SaveAuth(ctx context.Context, s auth.Settings) error
}

// LegacyCovers is the port to the covers cached before editions (media.Covers).
type LegacyCovers interface {
	// LegacyCoverIDs returns the games that still have a cover cached before editions.
	LegacyCoverIDs() []game.ID

	// SettleLegacyCover makes the game's main edition adopt its cover cached before editions
	// (true), or removes it (false), as the main edition's next resolution would.
	SettleLegacyCover(ctx context.Context, id game.ID) (adopted bool, err error)
}

// Service runs the upgrade.
type Service struct {
	games  Games
	tx     port.TxManager
	auth   AuthSettings
	covers LegacyCovers
	log    *slog.Logger
}

// Option configures the Service.
type Option func(*Service)

// WithLogger sets where the upgrade reports what it did (nowhere by default).
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// NewService builds the upgrade over the stored games (rewritten in transactions of tx), the
// security settings and the covers cached before editions.
func NewService(games Games, tx port.TxManager, authSettings AuthSettings, covers LegacyCovers, opts ...Option) *Service {
	s := &Service{
		games:  games,
		tx:     tx,
		auth:   authSettings,
		covers: covers,
		log:    slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Run upgrades what is still stored in an older shape. It returns nothing: what it cannot upgrade
// stays readable by the converting readers, so a failure is logged as a warning (naming the game)
// and never stops the server from starting.
func (s *Service) Run(ctx context.Context) {
	// Games first: the covers are settled on the editions of the rewritten games.
	s.rewriteGames(ctx)
	s.rewriteAuth(ctx)
	s.settleCovers(ctx)
}

// rewriteGames reads every game stored in an older version and saves it as it is, so it is stored
// in the current one.
func (s *Service) rewriteGames(ctx context.Context) {
	ids, err := s.games.OutdatedIDs(ctx)
	if err != nil {
		s.log.Warn("upgrade: listing the games stored in an older format", "error", err)
		return
	}

	rewritten := 0

	for start := 0; start < len(ids); start += batchSize {
		batch := ids[start:min(start+batchSize, len(ids))]
		n := 0

		err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
			n = 0

			for _, id := range batch {
				if s.rewriteGame(ctx, id) {
					n++
				}
			}

			return nil
		})
		if err != nil {
			s.log.Warn("upgrade: saving games in the current format", "games", len(batch), "error", err)
			continue
		}

		rewritten += n
	}

	if rewritten > 0 {
		s.log.Info("upgrade: games rewritten in the current format", "count", rewritten)
	}
}

// rewriteGame reads and saves one game, and reports whether it was rewritten; a game that cannot be
// is left as it is, with a warning.
func (s *Service) rewriteGame(ctx context.Context, id game.ID) bool {
	g, err := s.games.Get(ctx, id)
	if err != nil {
		s.log.Warn("upgrade: reading a game stored in an older format", "game", id, "error", err)
		return false
	}

	if err := s.games.Save(ctx, g); err != nil {
		s.log.Warn("upgrade: saving a game in the current format", "game", id, "error", err)
		return false
	}

	return true
}

// rewriteAuth saves the security settings stored in an older shape as the reader converts them, so
// they keep their meaning. The values are never logged.
func (s *Service) rewriteAuth(ctx context.Context) {
	outdated, err := s.auth.AuthOutdated(ctx)
	if err != nil {
		s.log.Warn("upgrade: checking the security settings", "error", err)
		return
	}

	if !outdated {
		return
	}

	settings, err := s.auth.Auth(ctx)
	if err != nil {
		s.log.Warn("upgrade: reading the security settings", "error", err)
		return
	}

	if err := s.auth.SaveAuth(ctx, settings); err != nil {
		s.log.Warn("upgrade: saving the security settings", "error", err)
		return
	}

	s.log.Info("upgrade: security settings rewritten in the current format")
}

// settleCovers makes the main editions adopt the covers cached before editions, or removes them.
func (s *Service) settleCovers(ctx context.Context) {
	adopted, removed := 0, 0

	for _, id := range s.covers.LegacyCoverIDs() {
		ok, err := s.covers.SettleLegacyCover(ctx, id)
		if err != nil {
			s.log.Warn("upgrade: settling a cover cached before editions", "game", id, "error", err)
			continue
		}

		if ok {
			adopted++
		} else {
			removed++
		}
	}

	if adopted+removed > 0 {
		s.log.Info("upgrade: covers cached before editions settled", "adopted", adopted, "removed", removed)
	}
}
