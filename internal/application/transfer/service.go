// Package transfer implements bulk import and export of the catalog as files (CSV).
package transfer

import (
	"bytes"
	"context"
	"io"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
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

// Service exposes the import/export use cases.
type Service struct {
	games game.Repository
	tx    port.TxManager
	now   port.Clock
	codec Codec
}

// NewService builds the service around the file codec.
func NewService(games game.Repository, tx port.TxManager, now port.Clock, codec Codec) *Service {
	return &Service{games: games, tx: tx, now: now, codec: codec}
}

// Import consolidates the rows of a file into the catalog. Rows carry stable external ids, so
// importing the same file twice updates instead of duplicating.
func (s *Service) Import(ctx context.Context, content []byte) (source.SyncReport, error) {
	report := source.SyncReport{StartedAt: s.now()}

	copies, warnings, err := s.codec.Decode(bytes.NewReader(content))
	if err != nil {
		return report, err
	}

	report.Fetched, report.Warnings = len(copies), warnings
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		res := game.NewConsolidator(games).Apply("", copies, s.now())
		for _, g := range res.Changed {
			if err := s.games.Save(ctx, g); err != nil {
				return err
			}
		}

		report.CopiesAdded, report.CopiesUpdated = res.CopiesAdded, res.CopiesUpdated
		report.CopiesUnchanged, report.GamesCreated = res.CopiesUnchanged, res.GamesCreated
		report.Warnings = append(report.Warnings, res.Warnings...)

		return nil
	})
	report.FinishedAt = s.now()

	return report, err
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
