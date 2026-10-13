package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// DetailsStore implements media.DetailsStore with one JSON row per game and language.
type DetailsStore struct{ db *DB }

// NewDetailsStore returns the repository backed by db.
func NewDetailsStore(db *DB) *DetailsStore { return &DetailsStore{db: db} }

var _ media.DetailsStore = (*DetailsStore)(nil)

// Get returns the stored details of a game in a language.
func (s *DetailsStore) Get(ctx context.Context, id game.ID, language string) (media.GameDetails, bool, error) {
	var raw string

	err := s.db.conn(ctx).QueryRowContext(ctx, `SELECT data FROM game_details WHERE game_id = ? AND language = ?`, id, language).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return media.GameDetails{}, false, nil
	}

	if err != nil {
		return media.GameDetails{}, false, err
	}

	var doc detailsDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return media.GameDetails{}, false, nil // unreadable cache entry: fetch again
	}

	return doc.details(), true, nil
}

// Put stores the details of a game, replacing the previous ones for that language.
func (s *DetailsStore) Put(ctx context.Context, id game.ID, d media.GameDetails) error {
	raw, err := json.Marshal(detailsDocOf(d))
	if err != nil {
		return err
	}

	_, err = s.db.conn(ctx).ExecContext(ctx, `INSERT INTO game_details (game_id, language, data, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(game_id, language) DO UPDATE SET data = excluded.data, fetched_at = excluded.fetched_at`,
		id, d.Language, string(raw), formatTime(d.FetchedAt))

	return err
}

// Delete removes every stored language of a game's details.
func (s *DetailsStore) Delete(ctx context.Context, id game.ID) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `DELETE FROM game_details WHERE game_id = ?`, id)
	return err
}

// Summaries returns the short summary of every game with details in a language.
func (s *DetailsStore) Summaries(ctx context.Context, language string) (map[game.ID]media.Summary, error) {
	rows, err := s.db.conn(ctx).QueryContext(ctx, `SELECT game_id, data, fetched_at FROM game_details WHERE language = ?`, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[game.ID]media.Summary{}

	for rows.Next() {
		var (
			id           game.ID
			raw, fetched string
		)
		if err := rows.Scan(&id, &raw, &fetched); err != nil {
			return nil, err
		}

		var d detailsDoc
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}

		out[id] = media.Summary{
			Genres:      d.Genres,
			ReleaseDate: d.ReleaseDate,
			FetchedAt:   parseTime(fetched),
		}
	}

	return out, rows.Err()
}

// detailsDoc is the stored form of a media.GameDetails. Its field names are the ones the
// application struct had when the cache was first written, without tags, and never change: every
// field is written, empty ones included, in this order (see TestStoredFormats_details).
type detailsDoc struct {
	Summary         string          `json:"Summary"`
	SummarySource   attributionDoc  `json:"SummarySource"`
	Genres          []string        `json:"Genres"`
	Developers      []string        `json:"Developers"`
	Publishers      []string        `json:"Publishers"`
	ReleaseDate     string          `json:"ReleaseDate"`
	AgeRating       string          `json:"AgeRating"`
	Players         string          `json:"Players"`
	Metacritic      int             `json:"Metacritic"`
	MetacriticURL   string          `json:"MetacriticURL"`
	Website         string          `json:"Website"`
	StoreURL        string          `json:"StoreURL"`
	Videos          []videoDoc      `json:"Videos"`
	Screenshots     []screenshotDoc `json:"Screenshots"`
	Sources         []provider.ID   `json:"Sources"`
	NotFoundBy      []provider.ID   `json:"NotFoundBy"`
	FallbackSkipped bool            `json:"FallbackSkipped"`
	LogicVersion    int             `json:"LogicVersion"`
	Language        string          `json:"Language"`
	FetchedAt       time.Time       `json:"FetchedAt"`
}

// attributionDoc is the stored form of a media.Attribution.
type attributionDoc struct {
	Name       string `json:"Name"`
	URL        string `json:"URL"`
	License    string `json:"License"`
	LicenseURL string `json:"LicenseURL"`
}

// videoDoc is the stored form of a media.Video.
type videoDoc struct {
	Title      string `json:"Title"`
	Thumbnail  string `json:"Thumbnail"`
	HLSURL     string `json:"HLSURL"`
	YouTubeID  string `json:"YouTubeID"`
	ThumbAsset string `json:"ThumbAsset"`
}

// screenshotDoc is the stored form of a media.Screenshot.
type screenshotDoc struct {
	ThumbURL   string `json:"ThumbURL"`
	FullURL    string `json:"FullURL"`
	ThumbAsset string `json:"ThumbAsset"`
	FullAsset  string `json:"FullAsset"`
}

func detailsDocOf(d media.GameDetails) detailsDoc {
	doc := detailsDoc{
		Summary:         d.Summary,
		SummarySource:   attributionDoc(d.SummarySource),
		Genres:          d.Genres,
		Developers:      d.Developers,
		Publishers:      d.Publishers,
		ReleaseDate:     d.ReleaseDate,
		AgeRating:       d.AgeRating,
		Players:         d.Players,
		Metacritic:      d.Metacritic,
		MetacriticURL:   d.MetacriticURL,
		Website:         d.Website,
		StoreURL:        d.StoreURL,
		Sources:         d.Sources,
		NotFoundBy:      d.NotFoundBy,
		FallbackSkipped: d.FallbackSkipped,
		LogicVersion:    d.LogicVersion,
		Language:        d.Language,
		FetchedAt:       d.FetchedAt,
	}

	// A nil list is written as null, an empty one as [], as before this type existed.
	if d.Videos != nil {
		doc.Videos = make([]videoDoc, 0, len(d.Videos))
		for _, v := range d.Videos {
			doc.Videos = append(doc.Videos, videoDoc(v))
		}
	}

	if d.Screenshots != nil {
		doc.Screenshots = make([]screenshotDoc, 0, len(d.Screenshots))
		for _, sc := range d.Screenshots {
			doc.Screenshots = append(doc.Screenshots, screenshotDoc(sc))
		}
	}

	return doc
}

func (doc detailsDoc) details() media.GameDetails {
	d := media.GameDetails{
		Summary:         doc.Summary,
		SummarySource:   media.Attribution(doc.SummarySource),
		Genres:          doc.Genres,
		Developers:      doc.Developers,
		Publishers:      doc.Publishers,
		ReleaseDate:     doc.ReleaseDate,
		AgeRating:       doc.AgeRating,
		Players:         doc.Players,
		Metacritic:      doc.Metacritic,
		MetacriticURL:   doc.MetacriticURL,
		Website:         doc.Website,
		StoreURL:        doc.StoreURL,
		Sources:         doc.Sources,
		NotFoundBy:      doc.NotFoundBy,
		FallbackSkipped: doc.FallbackSkipped,
		LogicVersion:    doc.LogicVersion,
		Language:        doc.Language,
		FetchedAt:       doc.FetchedAt,
	}

	if doc.Videos != nil {
		d.Videos = make([]media.Video, 0, len(doc.Videos))
		for _, v := range doc.Videos {
			d.Videos = append(d.Videos, media.Video(v))
		}
	}

	if doc.Screenshots != nil {
		d.Screenshots = make([]media.Screenshot, 0, len(doc.Screenshots))
		for _, sc := range doc.Screenshots {
			d.Screenshots = append(d.Screenshots, media.Screenshot(sc))
		}
	}

	return d
}
