package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
)

// HomeSnapshots implements home.Snapshots with one row per block.
type HomeSnapshots struct{ db *DB }

var _ home.Snapshots = (*HomeSnapshots)(nil)

// NewHomeSnapshots returns the repository backed by db.
func NewHomeSnapshots(db *DB) *HomeSnapshots { return &HomeSnapshots{db: db} }

// Snapshot implements home.Snapshots. A document that cannot be read counts as missing: the snapshot
// is a cache, and the job gathers the block again.
func (r *HomeSnapshots) Snapshot(ctx context.Context, b home.Block) (home.Snapshot, bool, error) {
	var raw, fetched, msg, kind, failed string

	err := r.db.conn(ctx).QueryRowContext(ctx,
		`SELECT doc, fetched_at, error, error_kind, failed_at FROM home_snapshots WHERE block = ?`, b).
		Scan(&raw, &fetched, &msg, &kind, &failed)
	if errors.Is(err, sql.ErrNoRows) {
		return home.Snapshot{Block: b}, false, nil
	}

	if err != nil {
		return home.Snapshot{}, false, err
	}

	var doc snapshotDoc
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return home.Snapshot{Block: b}, false, nil
	}

	return home.Snapshot{
		Block:          b,
		Featured:       dealsOf(doc.Featured),
		HistoricalLows: dealsOf(doc.HistoricalLows),
		FromSeries:     releasesOf(doc.FromSeries),
		Anticipated:    releasesOf(doc.Anticipated),
		FetchedAt:      parseTime(fetched),
		Error:          msg,
		ErrorKind:      home.ErrorKind(kind),
		FailedAt:       parseTime(failed),
		AttemptedAt:    parseTime(doc.AttemptedAt),
	}, true, nil
}

// SaveSnapshot implements home.Snapshots.
func (r *HomeSnapshots) SaveSnapshot(ctx context.Context, s home.Snapshot) error {
	raw, err := json.Marshal(snapshotDoc{
		V:              1,
		Featured:       dealDocsOf(s.Featured),
		HistoricalLows: dealDocsOf(s.HistoricalLows),
		FromSeries:     releaseDocsOf(s.FromSeries),
		Anticipated:    releaseDocsOf(s.Anticipated),
		AttemptedAt:    timeOrEmpty(s.AttemptedAt),
	})
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO home_snapshots (block, doc, fetched_at, error, error_kind, failed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(block) DO UPDATE SET doc = excluded.doc, fetched_at = excluded.fetched_at, error = excluded.error,
			error_kind = excluded.error_kind, failed_at = excluded.failed_at`,
		s.Block, string(raw), timeOrEmpty(s.FetchedAt), s.Error, string(s.ErrorKind), timeOrEmpty(s.FailedAt))

	return err
}

// timeOrEmpty stores a zero time as ”, which parseTime reads back as zero.
func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return formatTime(t)
}

// snapshotDoc is the stored form of a snapshot's lists. Field names never change, so rows written
// by an older version stay readable; a field added later reads as empty from them.
type snapshotDoc struct {
	V              int          `json:"v"`
	Featured       []dealDoc    `json:"featured"`
	HistoricalLows []dealDoc    `json:"historicalLows"`
	FromSeries     []releaseDoc `json:"fromSeries"`
	Anticipated    []releaseDoc `json:"anticipated"`

	// AttemptedAt is home.Snapshot.AttemptedAt. It lives in the document rather than in a column of
	// its own: it only feeds the refresh guard, and rows without it read as never interrupted.
	AttemptedAt string `json:"attemptedAt,omitempty"`
}

// dealDoc is the stored form of a game.Deal.
type dealDoc struct {
	ProviderGameID  string   `json:"providerGameId"`
	Title           string   `json:"title"`
	SteamAppID      string   `json:"steamAppId"`
	Shop            string   `json:"shop"`
	Price           int64    `json:"price"`
	PriceCurrency   string   `json:"priceCurrency"`
	Regular         int64    `json:"regular"`
	RegularCurrency string   `json:"regularCurrency"`
	Cut             int      `json:"cut"`
	HistoricalLow   bool     `json:"historicalLow"`
	Expires         string   `json:"expires"`
	URL             string   `json:"url"`
	ImageURL        string   `json:"imageUrl"`
	OwnedOn         []string `json:"ownedOn"`
}

// releaseDoc is the stored form of a game.Release.
type releaseDoc struct {
	ProviderGameID string   `json:"providerGameId"`
	Title          string   `json:"title"`
	Date           string   `json:"date"`
	Platforms      []string `json:"platforms"`
	CoverURL       string   `json:"coverUrl"`
	ArtURL         string   `json:"artUrl,omitempty"`
	URL            string   `json:"url"`
	Because        string   `json:"because"`
	BecauseTitle   string   `json:"becauseTitle"`
}

func dealDocsOf(in []game.Deal) []dealDoc {
	out := make([]dealDoc, 0, len(in))
	for _, d := range in {
		out = append(out, dealDoc{
			ProviderGameID:  d.ProviderGameID,
			Title:           d.Title,
			SteamAppID:      d.SteamAppID,
			Shop:            d.Shop,
			Price:           d.Price.Amount,
			PriceCurrency:   d.Price.Currency,
			Regular:         d.Regular.Amount,
			RegularCurrency: d.Regular.Currency,
			Cut:             d.Cut,
			HistoricalLow:   d.HistoricalLow,
			Expires:         timeOrEmpty(d.Expires),
			URL:             d.URL,
			ImageURL:        d.ImageURL,
			OwnedOn:         d.OwnedOn,
		})
	}

	return out
}

func dealsOf(in []dealDoc) []game.Deal {
	if len(in) == 0 {
		return nil
	}

	out := make([]game.Deal, 0, len(in))
	for _, d := range in {
		out = append(out, game.Deal{
			ProviderGameID: d.ProviderGameID,
			Title:          d.Title,
			SteamAppID:     d.SteamAppID,
			Shop:           d.Shop,
			Price: game.Money{
				Amount:   d.Price,
				Currency: d.PriceCurrency,
			},
			Regular: game.Money{
				Amount:   d.Regular,
				Currency: d.RegularCurrency,
			},
			Cut:           d.Cut,
			HistoricalLow: d.HistoricalLow,
			Expires:       parseTime(d.Expires),
			URL:           d.URL,
			ImageURL:      d.ImageURL,
			OwnedOn:       d.OwnedOn,
		})
	}

	return out
}

func releaseDocsOf(in []game.Release) []releaseDoc {
	out := make([]releaseDoc, 0, len(in))
	for _, r := range in {
		out = append(out, releaseDoc{
			ProviderGameID: r.ProviderGameID,
			Title:          r.Title,
			Date:           string(r.Date),
			Platforms:      r.Platforms,
			CoverURL:       r.CoverURL,
			ArtURL:         r.ArtURL,
			URL:            r.URL,
			Because:        string(r.Because),
			BecauseTitle:   r.BecauseTitle,
		})
	}

	return out
}

func releasesOf(in []releaseDoc) []game.Release {
	if len(in) == 0 {
		return nil
	}

	out := make([]game.Release, 0, len(in))
	for _, r := range in {
		out = append(out, game.Release{
			ProviderGameID: r.ProviderGameID,
			Title:          r.Title,
			Date:           game.Date(r.Date),
			Platforms:      r.Platforms,
			CoverURL:       r.CoverURL,
			ArtURL:         r.ArtURL,
			URL:            r.URL,
			Because:        game.ID(r.Because),
			BecauseTitle:   r.BecauseTitle,
		})
	}

	return out
}
