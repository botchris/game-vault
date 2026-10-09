package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Format versions of each document kind. Adding a field keeps a version; changing the meaning of
// one bumps it, and the decoder converts older documents when it reads them (see
// docs/superpowers/specs/2026-10-09-json-documents-design.md).
const (
	// gameDocVersion 2 replaced the copies' free-text condition with grade and contents.
	gameDocVersion     = 2
	sourceDocVersion   = 1
	providerDocVersion = 1
)

// errNewerDocument means a document was written by a newer Game Vault, whose fields this version
// would silently drop if it read and saved it again.
var errNewerDocument = errors.New("written by a newer version of Game Vault: update Game Vault to open this database")

func checkVersion(v, current int) error {
	if v > current {
		return fmt.Errorf("document version %d: %w", v, errNewerDocument)
	}

	return nil
}

// gameDoc is the stored form of a game.Game, copies included. Field names never change once
// released.
type gameDoc struct {
	V          int        `json:"v"`
	Title      string     `json:"title"`
	Links      game.Links `json:"links,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	CoverURL   string     `json:"coverUrl,omitempty"`
	CoverPhoto string     `json:"coverPhoto,omitempty"`
	CreatedAt  string     `json:"createdAt"`
	UpdatedAt  string     `json:"updatedAt"`
	Copies     []copyDoc  `json:"copies,omitempty"`
}

// copyDoc is the stored form of a game.Copy.
type copyDoc struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Platform   string `json:"platform,omitempty"`
	Status     string `json:"status"`
	Key        string `json:"key,omitempty"`
	RedeemBy   string `json:"redeemBy,omitempty"`
	Origin     string `json:"origin,omitempty"`
	AcquiredOn string `json:"acquiredOn,omitempty"`
	Edition    string `json:"edition,omitempty"`

	// Condition is the free-text condition of version-1 documents, converted when read; never
	// written.
	Condition     string     `json:"condition,omitempty"`
	Grade         string     `json:"grade,omitempty"`
	Contents      []string   `json:"contents,omitempty"`
	Location      string     `json:"location,omitempty"`
	Barcode       string     `json:"barcode,omitempty"`
	PriceAmount   int64      `json:"priceAmount,omitempty"`
	PriceCurrency string     `json:"priceCurrency,omitempty"`
	Notes         string     `json:"notes,omitempty"`
	Photos        []photoDoc `json:"photos,omitempty"`
	SourceID      string     `json:"sourceId,omitempty"`
	ExternalID    string     `json:"externalId,omitempty"`
	CreatedAt     string     `json:"createdAt"`
	UpdatedAt     string     `json:"updatedAt"`
}

// photoDoc is the stored form of a game.Photo.
type photoDoc struct {
	ID      string `json:"id"`
	Caption string `json:"caption,omitempty"`
	TakenAt string `json:"takenAt,omitempty"`
	AddedAt string `json:"addedAt"`
}

// sourceDoc is the stored form of a source.Source.
type sourceDoc struct {
	V                   int             `json:"v"`
	Type                string          `json:"type"`
	Name                string          `json:"name"`
	Enabled             bool            `json:"enabled"`
	SyncIntervalSeconds int64           `json:"syncIntervalSeconds,omitempty"`
	Settings            source.Settings `json:"settings,omitempty"`
	LastSync            *syncReportDoc  `json:"lastSync,omitempty"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

// syncReportDoc is the stored form of source.SyncReport (the same fields, so the types convert).
type syncReportDoc struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	Err             string    `json:"error,omitempty"`
	Fetched         int       `json:"fetched"`
	CopiesAdded     int       `json:"copiesAdded"`
	CopiesUpdated   int       `json:"copiesUpdated"`
	CopiesUnchanged int       `json:"copiesUnchanged"`
	GamesCreated    int       `json:"gamesCreated"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// providerDoc is the stored form of a provider.Provider.
type providerDoc struct {
	V         int             `json:"v"`
	Kind      string          `json:"kind"`
	Enabled   bool            `json:"enabled"`
	Priority  int             `json:"priority"`
	Settings  schema.Settings `json:"settings,omitempty"`
	UpdatedAt string          `json:"updatedAt"`
}

func encode(doc any) (string, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func encodeGame(g *game.Game) (string, error) {
	info := g.Info()
	doc := gameDoc{
		V:          gameDocVersion,
		Title:      info.Title,
		Links:      info.Links,
		Notes:      info.Notes,
		CoverURL:   info.CoverURL,
		CoverPhoto: string(info.CoverPhoto),
		CreatedAt:  formatTime(g.CreatedAt()),
		UpdatedAt:  formatTime(g.UpdatedAt()),
	}

	for _, c := range g.Copies() {
		doc.Copies = append(doc.Copies, copyDoc{
			ID:            string(c.ID),
			Kind:          string(c.Kind),
			Platform:      c.Platform,
			Status:        string(c.Status),
			Key:           c.Key,
			RedeemBy:      string(c.RedeemBy),
			Origin:        c.Origin,
			AcquiredOn:    string(c.AcquiredOn),
			Edition:       c.Edition,
			Grade:         string(c.Grade),
			Contents:      contentStrings(c.Contents),
			Location:      c.Location,
			Barcode:       string(c.Barcode),
			PriceAmount:   c.Price.Amount,
			PriceCurrency: c.Price.Currency,
			Notes:         c.Notes,
			Photos:        photoDocs(c.Photos),
			SourceID:      c.SourceID,
			ExternalID:    c.ExternalID,
			CreatedAt:     formatTime(c.CreatedAt),
			UpdatedAt:     formatTime(c.UpdatedAt),
		})
	}

	return encode(doc)
}

func decodeGame(id game.ID, raw string) (*game.Game, error) {
	var doc gameDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V, gameDocVersion); err != nil {
		return nil, err
	}

	copies := make([]game.Copy, 0, len(doc.Copies))
	for _, c := range doc.Copies {
		contents, err := contentsOf(c.Contents)
		if err != nil {
			return nil, err
		}

		cp := game.Copy{
			ID: game.ID(c.ID),
			CopyDetails: game.CopyDetails{
				Kind:       game.Kind(c.Kind),
				Platform:   c.Platform,
				Status:     game.Status(c.Status),
				Key:        c.Key,
				RedeemBy:   game.Date(c.RedeemBy),
				Origin:     c.Origin,
				AcquiredOn: game.Date(c.AcquiredOn),
				Edition:    c.Edition,
				Grade:      game.Grade(c.Grade),
				Contents:   contents,
				Location:   c.Location,
				Barcode:    game.Barcode(c.Barcode),
				Price: game.Money{
					Amount:   c.PriceAmount,
					Currency: c.PriceCurrency,
				},
				Notes: c.Notes,
			},
			Photos:     photosOf(c.Photos),
			SourceID:   c.SourceID,
			ExternalID: c.ExternalID,
			CreatedAt:  parseTime(c.CreatedAt),
			UpdatedAt:  parseTime(c.UpdatedAt),
		}

		if doc.V < 2 && strings.TrimSpace(c.Condition) != "" {
			if grade, contents, ok := convertCondition(c.Condition); ok {
				cp.Grade, cp.Contents = grade, contents
			} else {
				cp.Notes = strings.TrimSpace(cp.Notes + "\nCondition: " + strings.TrimSpace(c.Condition))
			}
		}

		copies = append(copies, cp)
	}

	info := game.Info{
		Title:      doc.Title,
		Links:      doc.Links,
		Notes:      doc.Notes,
		CoverURL:   doc.CoverURL,
		CoverPhoto: game.PhotoID(doc.CoverPhoto),
	}

	return game.Rehydrate(id, info, copies, parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt)), nil
}

func encodeSource(s *source.Source) (string, error) {
	doc := sourceDoc{
		V:                   sourceDocVersion,
		Type:                string(s.Type()),
		Name:                s.Name(),
		Enabled:             s.Enabled(),
		SyncIntervalSeconds: int64(s.SyncInterval() / time.Second),
		Settings:            s.Settings(),
		CreatedAt:           formatTime(s.CreatedAt()),
		UpdatedAt:           formatTime(s.UpdatedAt()),
	}

	if rep := s.LastSync(); rep != nil {
		r := syncReportDoc(*rep)
		doc.LastSync = &r
	}

	return encode(doc)
}

func decodeSource(id source.ID, raw string) (*source.Source, error) {
	var doc sourceDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V, sourceDocVersion); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = source.Settings{}
	}

	var lastSync *source.SyncReport

	if doc.LastSync != nil {
		r := source.SyncReport(*doc.LastSync)
		lastSync = &r
	}

	return source.Rehydrate(id, source.Type(doc.Type), doc.Name, doc.Enabled,
		time.Duration(doc.SyncIntervalSeconds)*time.Second, doc.Settings, lastSync,
		parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt)), nil
}

func encodeProvider(p *provider.Provider) (string, error) {
	return encode(providerDoc{
		V:         providerDocVersion,
		Kind:      string(p.Kind()),
		Enabled:   p.Enabled(),
		Priority:  p.Priority(),
		Settings:  p.Settings(),
		UpdatedAt: formatTime(p.UpdatedAt()),
	})
}

func decodeProvider(id provider.ID, raw string) (*provider.Provider, error) {
	var doc providerDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V, providerDocVersion); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = schema.Settings{}
	}

	return provider.Rehydrate(id, provider.Kind(doc.Kind), doc.Enabled, doc.Priority, doc.Settings, parseTime(doc.UpdatedAt)), nil
}

func contentStrings(c game.Contents) []string {
	out := make([]string, 0, len(game.AllContents))
	for _, x := range c.List() {
		out = append(out, string(x))
	}

	return out
}

func contentsOf(raw []string) (game.Contents, error) {
	parts := make([]game.Content, 0, len(raw))
	for _, s := range raw {
		parts = append(parts, game.Content(s))
	}

	return game.ContentsOf(parts...)
}

// oldConditions maps the texts the copy form suggested before version 2 (in the UI language of the
// time: English or Spanish) to a grade and contents.
var oldConditions = map[string]struct {
	grade    game.Grade
	contents []game.Content
}{
	"sealed":                   {game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"precintado":               {game.GradeSealed, []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"complete (case + manual)": {"", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"completo (caja + manual)": {"", []game.Content{game.ContentBox, game.ContentManual, game.ContentMedia}},
	"case and disc":            {"", []game.Content{game.ContentBox, game.ContentMedia}},
	"caja y disco":             {"", []game.Content{game.ContentBox, game.ContentMedia}},
	"disc only":                {"", []game.Content{game.ContentMedia}},
	"sólo disco":               {"", []game.Content{game.ContentMedia}},
	"damaged":                  {game.GradeDamaged, nil},
	"dañado":                   {game.GradeDamaged, nil},
}

// convertCondition reads a version-1 condition; ok is false for text that is not one of them.
func convertCondition(text string) (game.Grade, game.Contents, bool) {
	old, ok := oldConditions[strings.ToLower(strings.TrimSpace(text))]
	if !ok {
		return "", 0, false
	}

	contents, _ := game.ContentsOf(old.contents...) // the table only holds known contents

	return old.grade, contents, true
}

func photoDocs(photos []game.Photo) []photoDoc {
	if len(photos) == 0 {
		return nil
	}

	out := make([]photoDoc, 0, len(photos))
	for _, p := range photos {
		d := photoDoc{
			ID:      string(p.ID),
			Caption: p.Caption,
			AddedAt: formatTime(p.AddedAt),
		}
		if !p.TakenAt.IsZero() {
			d.TakenAt = formatTime(p.TakenAt)
		}

		out = append(out, d)
	}

	return out
}

func photosOf(docs []photoDoc) []game.Photo {
	if len(docs) == 0 {
		return nil
	}

	out := make([]game.Photo, 0, len(docs))
	for _, d := range docs {
		out = append(out, game.Photo{
			ID:      game.PhotoID(d.ID),
			Caption: d.Caption,
			TakenAt: parseTime(d.TakenAt),
			AddedAt: parseTime(d.AddedAt),
		})
	}

	return out
}
