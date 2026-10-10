// Package transfer implements bulk import and export of the catalog as files (CSV).
package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/settings"
	"gamevault/internal/domain/source"
)

// Codec is the port that converts between a file format and the domain.
type Codec interface {
	// Decode reads copies from r. Warnings are rows that could not be read.
	Decode(r io.Reader) (copies []game.ImportedCopy, warnings []string, err error)

	// Encode writes every copy of the games to w.
	Encode(w io.Writer, games []*game.Game) error

	// Extension is the file extension, without dot, e.g. "csv".
	Extension() string
}

// CoverCache is the port used to drop the cached covers of games the import changed.
type CoverCache interface {
	// Invalidate drops the game's cached covers and details, so the next request resolves them again.
	Invalidate(ctx context.Context, id game.ID) error

	// InvalidateEdition drops the cached cover of the game's edition on system.
	InvalidateEdition(ctx context.Context, id game.ID, system string) error
}

// Service exposes the import/export use cases.
type Service struct {
	games game.Repository
	tx    port.TxManager
	prefs settings.Repository
	now   port.Clock
	codec Codec

	covers CoverCache
}

// NewService builds the service around the file codec. The preferences give the currency of rows
// that have a price but no currency. covers may be nil when no cover cache is wired.
func NewService(games game.Repository, tx port.TxManager, prefs settings.Repository, now port.Clock, codec Codec, covers CoverCache) *Service {
	return &Service{
		games: games,
		tx:    tx,
		prefs: prefs,
		now:   now,
		codec: codec,

		covers: covers,
	}
}

// Import consolidates the rows of a file into the catalog. Rows carry stable external ids, so
// importing the same file twice updates instead of duplicating.
func (s *Service) Import(ctx context.Context, content []byte) (source.SyncReport, error) {
	report := source.SyncReport{StartedAt: s.now()}

	copies, warnings, err := s.codec.Decode(bytes.NewReader(content))
	if err != nil {
		return report, err
	}

	warnings, err = s.defaultCurrency(ctx, copies, warnings)
	if err != nil {
		return report, err
	}

	report.Fetched, report.Warnings = len(copies), warnings

	var (
		prints = map[game.ID]map[string]string{}
		stale  = map[game.ID][]string{}
	)

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		// Start over on every attempt, so a retried transaction reports only what it saved.
		clear(prints)
		clear(stale)

		for _, g := range games {
			prints[g.ID()] = g.CoverFingerprints()
		}

		res := game.NewConsolidator(games).Apply("", copies, s.now())
		for _, g := range res.Changed {
			if err := s.games.Save(ctx, g); err != nil {
				return err
			}

			// A new game has no fingerprints before, so all its editions count.
			if changed := game.ChangedSystems(prints[g.ID()], g.CoverFingerprints()); len(changed) > 0 {
				stale[g.ID()] = changed
			}
		}

		report.CopiesAdded, report.CopiesUpdated = res.CopiesAdded, res.CopiesUpdated
		report.CopiesUnchanged, report.GamesCreated = res.CopiesUnchanged, res.GamesCreated
		report.Warnings = append(report.Warnings, res.Warnings...)

		return nil
	})
	if err == nil {
		s.invalidateEditions(ctx, stale)
	}

	report.FinishedAt = s.now()

	return report, err
}

// invalidateEditions drops the cached covers of the editions the import changed. Best effort: a
// stale cached image is not worth failing the import.
func (s *Service) invalidateEditions(ctx context.Context, stale map[game.ID][]string) {
	if s.covers == nil {
		return
	}

	for id, systems := range stale {
		for _, system := range systems {
			_ = s.covers.InvalidateEdition(ctx, id, system)
		}
	}
}

// Export writes the whole catalog, one row per copy. Returns the suggested file name and content.
func (s *Service) Export(ctx context.Context) (string, []byte, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	if err := s.codec.Encode(&buf, games); err != nil {
		return "", nil, err
	}

	return "gamevault-" + string(game.DateOf(s.now())) + "." + s.codec.Extension(), buf.Bytes(), nil
}

// defaultCurrency gives the default currency to the prices that came without one. The codec read
// them with two decimals, so a default currency with other decimals rescales them. Without a
// default currency those prices are dropped, with one warning.
func (s *Service) defaultCurrency(ctx context.Context, copies []game.ImportedCopy, warnings []string) ([]string, error) {
	prefs, err := s.prefs.Preferences(ctx)
	if err != nil {
		return warnings, err
	}

	missing := 0

	for i := range copies {
		p := &copies[i].Details.Price
		if p.IsZero() || p.Currency != "" {
			continue
		}

		if prefs.Currency == "" {
			*p = game.Money{}
			missing++

			continue
		}

		p.Amount = rescale(p.Amount, 2, game.CurrencyDigits(prefs.Currency))
		p.Currency = prefs.Currency
	}

	if missing > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d prices have no currency and no default currency is set (System → Preferences): they were not imported", missing))
	}

	return warnings, nil
}

// rescale moves an amount from one number of decimals to another (12.50 with 2 → 13 with 0, half up).
func rescale(amount int64, from, to int) int64 {
	for ; from < to; from++ {
		amount *= 10
	}

	for ; from > to; from-- {
		amount = (amount + 5) / 10
	}

	return amount
}
