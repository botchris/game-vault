package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// Format versions of each document kind. Adding a field keeps a version; changing the meaning of
// one bumps it (see docs/superpowers/specs/2026-10-09-json-documents-design.md). The decoders read
// only the current version: an older document is refused rather than read with fields missing.
const (
	// gameDocVersion 2 replaced the copies' free-text condition with grade and contents; 3 moved the
	// cover to editions (covers by system, mainSystem).
	gameDocVersion     = 3
	sourceDocVersion   = 1
	providerDocVersion = 1
	fieldsDocVersion   = 1

	connectionDocVersion = 1
)

// errNewerDocument means a document was written by a newer Game Vault, whose fields this version
// would silently drop if it read and saved it again.
var errNewerDocument = errors.New("written by a newer version of Game Vault: update Game Vault to open this database")

// errOlderDocument means a document is in a version this Game Vault no longer reads: the versions
// that converted it are no longer published, so reading it would silently lose what it holds.
var errOlderDocument = errors.New("no longer supported: this database was written by a Game Vault too old to open")

func checkVersion(v, current int) error {
	if v > current {
		return fmt.Errorf("document version %d: %w", v, errNewerDocument)
	}

	return nil
}

// checkGameVersion refuses game documents of any version but the current one: newer ones as
// checkVersion does, older ones because their fields (condition, the single cover) are no longer
// read.
func checkGameVersion(v int) error {
	if v < gameDocVersion {
		return fmt.Errorf("game document version %d is %w", v, errOlderDocument)
	}

	return checkVersion(v, gameDocVersion)
}

// gameDoc is the stored form of a game.Game, copies included. Field names never change once
// released.
type gameDoc struct {
	V     int        `json:"v"`
	Title string     `json:"title"`
	Links game.Links `json:"links,omitempty"`
	Notes string     `json:"notes,omitempty"`

	// Covers are the covers chosen per system; MainSystem the user's main edition.
	Covers     map[string]coverDoc `json:"covers,omitempty"`
	MainSystem string              `json:"mainSystem,omitempty"`
	PlayStatus string              `json:"playStatus,omitempty"`
	Rating     int                 `json:"rating,omitempty"`

	Fields map[string]fieldValueDoc `json:"fields,omitempty"`

	// AutoLinks are the keys of the links found automatically, RejectedLinks the automatic links
	// the user removed, AutoLinkTriedAt the last automatic search (see game.Game.AutoLinks). Added
	// to version 3 without changing it: older documents simply have none.
	AutoLinks       []string     `json:"autoLinks,omitempty"`
	RejectedLinks   []linkRefDoc `json:"rejectedLinks,omitempty"`
	AutoLinkTriedAt string       `json:"autoLinkTriedAt,omitempty"`

	CreatedAt string    `json:"createdAt"`
	UpdatedAt string    `json:"updatedAt"`
	Copies    []copyDoc `json:"copies,omitempty"`
}

// linkRefDoc is the stored form of a game.LinkRef.
type linkRefDoc struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

// coverDoc is the stored form of a game.EditionCover: a URL or a photo id.
type coverDoc struct {
	URL   string `json:"url,omitempty"`
	Photo string `json:"photo,omitempty"`
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

	Grade         string        `json:"grade,omitempty"`
	Contents      []string      `json:"contents,omitempty"`
	Location      string        `json:"location,omitempty"`
	Barcode       string        `json:"barcode,omitempty"`
	PriceAmount   int64         `json:"priceAmount,omitempty"`
	PriceCurrency string        `json:"priceCurrency,omitempty"`
	Notes         string        `json:"notes,omitempty"`
	System        string        `json:"system,omitempty"`
	SourceSystem  string        `json:"sourceSystem,omitempty"`
	Photos        []photoDoc    `json:"photos,omitempty"`
	Estimates     []estimateDoc `json:"estimates,omitempty"`
	NextValuation string        `json:"nextValuation,omitempty"`
	ValuedAt      string        `json:"valuedAt,omitempty"`

	Fields     map[string]fieldValueDoc `json:"fields,omitempty"`
	SourceID   string                   `json:"sourceId,omitempty"`
	ExternalID string                   `json:"externalId,omitempty"`
	CreatedAt  string                   `json:"createdAt"`
	UpdatedAt  string                   `json:"updatedAt"`
}

// photoDoc is the stored form of a game.Photo.
type photoDoc struct {
	ID      string `json:"id"`
	Caption string `json:"caption,omitempty"`
	TakenAt string `json:"takenAt,omitempty"`
	AddedAt string `json:"addedAt"`
}

// estimateDoc is the stored form of a game.Estimate: one currency, amounts in its minor units.
type estimateDoc struct {
	Provider  string `json:"provider"`
	Currency  string `json:"currency"`
	Sell      int64  `json:"sell"`
	BuyCash   int64  `json:"buyCash,omitempty"`
	BuyCredit int64  `json:"buyCredit,omitempty"`
	Listings  int    `json:"listings,omitempty"`
	URL       string `json:"url,omitempty"`
	FetchedAt string `json:"fetchedAt"`
}

// sourceDoc is the stored form of a source.Source.
type sourceDoc struct {
	V                   int             `json:"v"`
	Type                string          `json:"type"`
	Name                string          `json:"name"`
	Enabled             bool            `json:"enabled"`
	SyncIntervalSeconds int64           `json:"syncIntervalSeconds,omitempty"`
	Settings            schema.Settings `json:"settings,omitempty"`
	LastSync            *syncReportDoc  `json:"lastSync,omitempty"`
	Exclusions          []exclusionDoc  `json:"exclusions,omitempty"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

// exclusionDoc is the stored form of a source.Exclusion.
type exclusionDoc struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title,omitempty"`
	At         string `json:"at"`
}

// syncReportDoc is the stored form of source.SyncReport (the same fields, so the types convert).
type syncReportDoc struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	Err             string    `json:"error,omitempty"`
	SignedOut       bool      `json:"signedOut,omitempty"`
	FailingSince    time.Time `json:"failingSince,omitzero"`
	Fetched         int       `json:"fetched"`
	CopiesAdded     int       `json:"copiesAdded"`
	CopiesUpdated   int       `json:"copiesUpdated"`
	CopiesUnchanged int       `json:"copiesUnchanged"`
	GamesCreated    int       `json:"gamesCreated"`
	Excluded        int       `json:"excluded,omitempty"`
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

// connectionDoc is the stored form of a connection.Connection.
type connectionDoc struct {
	V         int             `json:"v"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Enabled   bool            `json:"enabled"`
	Events    []string        `json:"events"`
	Language  string          `json:"language"`
	Settings  schema.Settings `json:"settings,omitempty"`
	PausedWhy string          `json:"pausedWhy,omitempty"`
	PausedAt  string          `json:"pausedAt,omitempty"`
	CreatedAt string          `json:"createdAt"`
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
		Covers:     coversToDoc(g.Covers()),
		MainSystem: g.MainSystem(),
		PlayStatus: string(info.PlayStatus),
		Rating:     int(info.Rating),
		Fields:     fieldsToDoc(g.Fields()),
		AutoLinks:  g.AutoLinks(),
		CreatedAt:  formatTime(g.CreatedAt()),
		UpdatedAt:  formatTime(g.UpdatedAt()),
	}
	if t := g.AutoLinkTriedAt(); !t.IsZero() {
		doc.AutoLinkTriedAt = formatTime(t)
	}

	for _, r := range g.RejectedLinks() {
		doc.RejectedLinks = append(doc.RejectedLinks, linkRefDoc{
			Key: r.Key,
			ID:  r.ID,
		})
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
			System:        c.CopyDetails.System,
			SourceSystem:  c.SourceSystem,
			Photos:        photoDocs(c.Photos),
			Estimates:     estimateDocs(c.Estimates),
			NextValuation: optionalTime(c.NextValuation),
			ValuedAt:      optionalTime(c.ValuedAt),
			Fields:        fieldsToDoc(c.Fields),
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

	if err := checkGameVersion(doc.V); err != nil {
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
				Notes:  c.Notes,
				System: c.System,
			},
			Photos:        photosOf(c.Photos),
			Estimates:     estimatesOf(c.Estimates),
			NextValuation: parseTime(c.NextValuation),
			ValuedAt:      parseTime(c.ValuedAt),
			Fields:        fieldsFromDoc(c.Fields),
			SourceID:      c.SourceID,
			ExternalID:    c.ExternalID,
			SourceSystem:  c.SourceSystem,
			CreatedAt:     parseTime(c.CreatedAt),
			UpdatedAt:     parseTime(c.UpdatedAt),
		}

		copies = append(copies, cp)
	}

	rejected := make([]game.LinkRef, 0, len(doc.RejectedLinks))
	for _, r := range doc.RejectedLinks {
		rejected = append(rejected, game.LinkRef{
			Key: r.Key,
			ID:  r.ID,
		})
	}

	return game.Rehydrate(game.Snapshot{
		ID: id,
		Info: game.Info{
			Title:      doc.Title,
			Links:      doc.Links,
			Notes:      doc.Notes,
			PlayStatus: game.PlayStatus(doc.PlayStatus),
			Rating:     game.Rating(doc.Rating),
			Fields:     fieldsFromDoc(doc.Fields),
		},
		Copies:          copies,
		Covers:          coversFromDoc(doc.Covers),
		MainSystem:      doc.MainSystem,
		AutoLinks:       doc.AutoLinks,
		RejectedLinks:   rejected,
		AutoLinkTriedAt: parseTime(doc.AutoLinkTriedAt),
		CreatedAt:       parseTime(doc.CreatedAt),
		UpdatedAt:       parseTime(doc.UpdatedAt),
	}), nil
}

func coversToDoc(covers map[string]game.EditionCover) map[string]coverDoc {
	if len(covers) == 0 {
		return nil
	}

	out := make(map[string]coverDoc, len(covers))
	for system, c := range covers {
		out[system] = coverDoc{
			URL:   c.URL,
			Photo: string(c.Photo),
		}
	}

	return out
}

func coversFromDoc(docs map[string]coverDoc) map[string]game.EditionCover {
	if len(docs) == 0 {
		return nil
	}

	out := make(map[string]game.EditionCover, len(docs))
	for system, d := range docs {
		out[system] = game.EditionCover{
			URL:   d.URL,
			Photo: game.PhotoID(d.Photo),
		}
	}

	return out
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

	for _, e := range s.Exclusions() {
		doc.Exclusions = append(doc.Exclusions, exclusionDoc{
			ExternalID: e.ExternalID,
			Title:      e.Title,
			At:         formatTime(e.At),
		})
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
		doc.Settings = schema.Settings{}
	}

	var lastSync *source.SyncReport

	if doc.LastSync != nil {
		r := source.SyncReport(*doc.LastSync)
		lastSync = &r
	}

	exclusions := make([]source.Exclusion, 0, len(doc.Exclusions))
	for _, e := range doc.Exclusions {
		exclusions = append(exclusions, source.Exclusion{
			ExternalID: e.ExternalID,
			Title:      e.Title,
			At:         parseTime(e.At),
		})
	}

	return source.Rehydrate(source.Snapshot{
		ID:           id,
		Type:         source.Type(doc.Type),
		Name:         doc.Name,
		Enabled:      doc.Enabled,
		SyncInterval: time.Duration(doc.SyncIntervalSeconds) * time.Second,
		Settings:     doc.Settings,
		LastSync:     lastSync,
		Exclusions:   exclusions,
		CreatedAt:    parseTime(doc.CreatedAt),
		UpdatedAt:    parseTime(doc.UpdatedAt),
	}), nil
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
	out := make([]string, 0, len(game.AllContents()))
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

func photoDocs(photos []game.Photo) []photoDoc {
	if len(photos) == 0 {
		return nil
	}

	out := make([]photoDoc, 0, len(photos))
	for _, p := range photos {
		out = append(out, photoDoc{
			ID:      string(p.ID),
			Caption: p.Caption,
			TakenAt: optionalTime(p.TakenAt),
			AddedAt: formatTime(p.AddedAt),
		})
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

func optionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return formatTime(t)
}

func estimateDocs(estimates []game.Estimate) []estimateDoc {
	if len(estimates) == 0 {
		return nil
	}

	out := make([]estimateDoc, 0, len(estimates))
	for _, e := range estimates {
		out = append(out, estimateDoc{
			Provider:  e.Provider,
			Currency:  e.Sell.Currency,
			Sell:      e.Sell.Amount,
			BuyCash:   e.BuyCash.Amount,
			BuyCredit: e.BuyCredit.Amount,
			Listings:  e.Listings,
			URL:       e.URL,
			FetchedAt: formatTime(e.FetchedAt),
		})
	}

	return out
}

func estimatesOf(docs []estimateDoc) []game.Estimate {
	if len(docs) == 0 {
		return nil
	}

	money := func(amount int64, currency string) game.Money {
		if amount == 0 {
			return game.Money{}
		}

		return game.Money{
			Amount:   amount,
			Currency: currency,
		}
	}

	out := make([]game.Estimate, 0, len(docs))
	for _, d := range docs {
		out = append(out, game.Estimate{
			Provider:  d.Provider,
			Sell:      money(d.Sell, d.Currency),
			BuyCash:   money(d.BuyCash, d.Currency),
			BuyCredit: money(d.BuyCredit, d.Currency),
			Listings:  d.Listings,
			URL:       d.URL,
			FetchedAt: parseTime(d.FetchedAt),
		})
	}

	return out
}

// fieldValueDoc is the stored form of a game.FieldValue: only the member that is set.
type fieldValueDoc struct {
	Text     string   `json:"text,omitempty"`
	Bool     *bool    `json:"bool,omitempty"`
	Number   *int64   `json:"number,omitempty"`
	Amount   *int64   `json:"amount,omitempty"`
	Currency string   `json:"currency,omitempty"`
	Date     string   `json:"date,omitempty"`
	Minutes  *int64   `json:"minutes,omitempty"`
	Choice   string   `json:"choice,omitempty"`
	Choices  []string `json:"choices,omitempty"`
}

func fieldsToDoc(values game.FieldValues) map[string]fieldValueDoc {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]fieldValueDoc, len(values))
	for id, v := range values {
		d := fieldValueDoc{
			Text:    v.Text,
			Bool:    v.Bool,
			Number:  v.Number,
			Date:    v.Date,
			Minutes: v.Minutes,
			Choice:  v.Choice,
			Choices: v.Choices,
		}
		if v.Money != nil {
			amount := v.Money.Amount
			d.Amount, d.Currency = &amount, v.Money.Currency
		}

		out[id] = d
	}

	return out
}

func fieldsFromDoc(docs map[string]fieldValueDoc) game.FieldValues {
	if len(docs) == 0 {
		return nil
	}

	out := make(game.FieldValues, len(docs))
	for id, d := range docs {
		v := game.FieldValue{
			Text:    d.Text,
			Bool:    d.Bool,
			Number:  d.Number,
			Date:    d.Date,
			Minutes: d.Minutes,
			Choice:  d.Choice,
			Choices: d.Choices,
		}
		if d.Amount != nil {
			v.Money = &game.Money{
				Amount:   *d.Amount,
				Currency: d.Currency,
			}
		}

		out[id] = v
	}

	return out
}

// fieldsDoc is the stored form of a field.Set. Field names never change once released.
type fieldsDoc struct {
	V      int        `json:"v"`
	Fields []fieldDoc `json:"fields"`
}

// fieldDoc is the stored form of a field.Definition.
type fieldDoc struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Scope    string      `json:"scope"`
	Kinds    []string    `json:"kinds,omitempty"`
	Decimals int         `json:"decimals,omitempty"`
	Unit     string      `json:"unit,omitempty"`
	Currency string      `json:"currency,omitempty"`
	Choices  []choiceDoc `json:"choices,omitempty"`
}

// choiceDoc is the stored form of a field.Choice.
type choiceDoc struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// decodeFields reads a stored field.Set.
func decodeFields(raw string) (*field.Set, error) {
	var doc fieldsDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if doc.V != fieldsDocVersion {
		return nil, fmt.Errorf("custom fields document version %d: %w", doc.V, errNewerDocument)
	}

	defs := make([]field.Definition, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		d := field.Definition{
			ID:       f.ID,
			Name:     f.Name,
			Type:     field.Type(f.Type),
			Scope:    field.Scope(f.Scope),
			Decimals: f.Decimals,
			Unit:     f.Unit,
			Currency: f.Currency,
		}

		for _, k := range f.Kinds {
			d.Kinds = append(d.Kinds, game.Kind(k))
		}

		for _, c := range f.Choices {
			d.Choices = append(d.Choices, field.Choice{
				ID:   c.ID,
				Name: c.Name,
			})
		}

		defs = append(defs, d)
	}

	return field.NewSet(defs), nil
}

// encodeFields returns the stored form of a field.Set.
func encodeFields(s *field.Set) (string, error) {
	defs := s.Definitions()
	doc := fieldsDoc{
		V:      fieldsDocVersion,
		Fields: make([]fieldDoc, 0, len(defs)),
	}

	for _, d := range defs {
		f := fieldDoc{
			ID:       d.ID,
			Name:     d.Name,
			Type:     string(d.Type),
			Scope:    string(d.Scope),
			Decimals: d.Decimals,
			Unit:     d.Unit,
			Currency: d.Currency,
		}

		for _, k := range d.Kinds {
			f.Kinds = append(f.Kinds, string(k))
		}

		for _, c := range d.Choices {
			f.Choices = append(f.Choices, choiceDoc{
				ID:   c.ID,
				Name: c.Name,
			})
		}

		doc.Fields = append(doc.Fields, f)
	}

	return encode(doc)
}

func encodeConnection(c *connection.Connection) (string, error) {
	events := make([]string, 0, len(c.Events()))
	for _, k := range c.Events() {
		events = append(events, string(k))
	}

	return encode(connectionDoc{
		V:         connectionDocVersion,
		Type:      string(c.Type()),
		Name:      c.Name(),
		Enabled:   c.Enabled(),
		Events:    events,
		Language:  c.Language(),
		Settings:  c.Settings(),
		PausedWhy: c.PausedWhy(),
		PausedAt:  optionalTime(c.PausedAt()),
		CreatedAt: formatTime(c.CreatedAt()),
		UpdatedAt: formatTime(c.UpdatedAt()),
	})
}

func decodeConnection(id connection.ID, raw string) (*connection.Connection, error) {
	var doc connectionDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}

	if err := checkVersion(doc.V, connectionDocVersion); err != nil {
		return nil, err
	}

	if doc.Settings == nil {
		doc.Settings = schema.Settings{}
	}

	events := make([]event.Kind, 0, len(doc.Events))
	for _, k := range doc.Events {
		events = append(events, event.Kind(k))
	}

	return connection.Rehydrate(connection.Snapshot{
		ID:        id,
		Type:      connection.Type(doc.Type),
		Name:      doc.Name,
		Enabled:   doc.Enabled,
		Events:    events,
		Language:  doc.Language,
		Settings:  doc.Settings,
		PausedWhy: doc.PausedWhy,
		PausedAt:  parseTime(doc.PausedAt),
		CreatedAt: parseTime(doc.CreatedAt),
		UpdatedAt: parseTime(doc.UpdatedAt),
	}), nil
}
