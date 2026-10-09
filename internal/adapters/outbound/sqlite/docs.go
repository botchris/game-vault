package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// docVersion is the format version written into every document. Adding a field keeps it; changing
// the meaning of one bumps it, and the decoders convert older versions when they read them (see
// docs/superpowers/specs/2026-10-09-json-documents-design.md).
const docVersion = 1

// errNewerDocument means a document was written by a newer Game Vault, whose fields this version
// would silently drop if it read and saved it again.
var errNewerDocument = errors.New("written by a newer version of Game Vault: update Game Vault to open this database")

func checkVersion(v int) error {
	if v > docVersion {
		return fmt.Errorf("document version %d: %w", v, errNewerDocument)
	}

	return nil
}

// gameDoc is the stored form of a game.Game, copies included. Field names never change once
// released.
type gameDoc struct {
	V         int        `json:"v"`
	Title     string     `json:"title"`
	Links     game.Links `json:"links,omitempty"`
	Notes     string     `json:"notes,omitempty"`
	CoverURL  string     `json:"coverUrl,omitempty"`
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`
	Copies    []copyDoc  `json:"copies,omitempty"`
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
	Condition  string `json:"condition,omitempty"`
	Location   string `json:"location,omitempty"`
	Barcode    string `json:"barcode,omitempty"`
	Notes      string `json:"notes,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
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
		V:         docVersion,
		Title:     info.Title,
		Links:     info.Links,
		Notes:     info.Notes,
		CoverURL:  info.CoverURL,
		CreatedAt: formatTime(g.CreatedAt()),
		UpdatedAt: formatTime(g.UpdatedAt()),
	}

	for _, c := range g.Copies() {
		doc.Copies = append(doc.Copies, copyDoc{
			ID:         string(c.ID),
			Kind:       string(c.Kind),
			Platform:   c.Platform,
			Status:     string(c.Status),
			Key:        c.Key,
			RedeemBy:   string(c.RedeemBy),
			Origin:     c.Origin,
			AcquiredOn: string(c.AcquiredOn),
			Edition:    c.Edition,
			Location:   c.Location,
			Barcode:    string(c.Barcode),
			Notes:      c.Notes,
			SourceID:   c.SourceID,
			ExternalID: c.ExternalID,
			CreatedAt:  formatTime(c.CreatedAt),
			UpdatedAt:  formatTime(c.UpdatedAt),
		})
	}

	return encode(doc)
}

func decodeGame(id game.ID, raw string) (*game.Game, error) {
	var doc gameDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V); err != nil {
		return nil, err
	}

	copies := make([]game.Copy, 0, len(doc.Copies))
	for _, c := range doc.Copies {
		copies = append(copies, game.Copy{
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
				Location:   c.Location,
				Barcode:    game.Barcode(c.Barcode),
				Notes:      c.Notes,
			},
			SourceID:   c.SourceID,
			ExternalID: c.ExternalID,
			CreatedAt:  parseTime(c.CreatedAt),
			UpdatedAt:  parseTime(c.UpdatedAt),
		})
	}

	info := game.Info{
		Title:    doc.Title,
		Links:    doc.Links,
		Notes:    doc.Notes,
		CoverURL: doc.CoverURL,
	}

	return game.Rehydrate(id, info, copies, parseTime(doc.CreatedAt), parseTime(doc.UpdatedAt)), nil
}

func encodeSource(s *source.Source) (string, error) {
	doc := sourceDoc{
		V:                   docVersion,
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

	if err := checkVersion(doc.V); err != nil {
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
		V:         docVersion,
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

	if err := checkVersion(doc.V); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = schema.Settings{}
	}

	return provider.Rehydrate(id, provider.Kind(doc.Kind), doc.Enabled, doc.Priority, doc.Settings, parseTime(doc.UpdatedAt)), nil
}
