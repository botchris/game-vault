// Package source holds the Source aggregate: an external account (Humble Bundle, Steam...)
// that is scanned periodically to keep the catalog up to date, like Sonarr's import lists.
package source

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/schema"
)

// ID identifies a source.
type ID string

// NewID returns a fresh identifier.
func NewID() ID { return ID(uuid.Must(uuid.NewV7()).String()) }

// Type identifies the provider behind a source, e.g. "humble" or "steam".
type Type string

// The settings types are shared with other integrations, so they live in schema.
type (
	// Settings is a source's configuration (see schema.Settings).
	Settings = schema.Settings

	// Field describes one setting a source asks for (see schema.Field).
	Field = schema.Field

	// FieldKind is how a field is entered and stored (see schema.FieldKind).
	FieldKind = schema.FieldKind
)

// Secret masking and field kinds are shared with other integrations, so they live in schema.
const (
	SecretPlaceholder = schema.SecretPlaceholder
	FieldText         = schema.FieldText
	FieldSecret       = schema.FieldSecret
	FieldState        = schema.FieldState
)

// Errors returned by source lookups and creation. Callers match them with errors.Is.
var (
	ErrNotFound    = errors.New("source not found")
	ErrUnknownType = errors.New("unknown source type")
)

// ValidationError reports input that breaks a domain rule.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// TypeDescriptor describes a source type and the settings it needs.
type TypeDescriptor struct {
	Type           Type
	Name           string
	DescriptionKey string
	Fields         schema.Fields

	// ManualScans makes new sources of this type scan only when asked (no schedule by default),
	// for stores where every request should be one the user chose to make.
	ManualScans bool
}

// Validate checks that every required setting is present.
func (d TypeDescriptor) Validate(s Settings) error { return d.Fields.Validate(s, d.Name) }

// MergeSettings applies incoming settings over stored ones (see schema.Fields.Merge).
func (d TypeDescriptor) MergeSettings(stored, incoming Settings) Settings {
	return d.Fields.Merge(stored, incoming)
}

// Masked returns the settings with secrets masked, safe to show to clients.
func (d TypeDescriptor) Masked(s Settings) Settings { return d.Fields.Masked(s) }

// SyncReport is the outcome of one scan.
type SyncReport struct {
	StartedAt       time.Time
	FinishedAt      time.Time
	Err             string
	Fetched         int
	CopiesAdded     int
	CopiesUpdated   int
	CopiesUnchanged int
	GamesCreated    int

	// Excluded counts the imported items skipped because the user removed them from the source.
	Excluded int
	Warnings []string
}

// Success reports whether the scan finished without error.
func (r SyncReport) Success() bool { return r.Err == "" }

// Source is a configured account that feeds the catalog.
type Source struct {
	id           ID
	typ          Type
	name         string
	enabled      bool
	syncInterval time.Duration
	settings     Settings
	lastSync     *SyncReport
	exclusions   []Exclusion // newest first
	createdAt    time.Time
	updatedAt    time.Time
}

// Config holds the user-editable attributes of a source.
type Config struct {
	Name         string
	Enabled      bool
	SyncInterval time.Duration // 0 = manual only
	Settings     Settings
}

// Exclusion is an item the user removed from a source's imports: the source skips it on every
// scan until the user brings it back.
type Exclusion struct {
	// ExternalID is the copy's id at the source, e.g. "psn:EP4350-CUSA00127_00-NETFLIXPOLLUX001".
	ExternalID string

	// Title is the title the item had, to list it.
	Title string

	// At is when it was removed.
	At time.Time
}

func (c Config) validate(d TypeDescriptor) (Config, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		c.Name = d.Name
	}

	if c.SyncInterval < 0 {
		return c, invalid("sync interval cannot be negative")
	}

	return c, d.Validate(c.Settings)
}

// New creates a source of the given type.
func New(d TypeDescriptor, cfg Config, now time.Time) (*Source, error) {
	cfg.Settings = d.MergeSettings(nil, cfg.Settings)

	cfg, err := cfg.validate(d)
	if err != nil {
		return nil, err
	}

	return &Source{
		id:           NewID(),
		typ:          d.Type,
		name:         cfg.Name,
		enabled:      cfg.Enabled,
		syncInterval: cfg.SyncInterval,
		settings:     cfg.Settings,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// Rehydrate rebuilds a source from storage. Only repositories should call it.
func Rehydrate(id ID, typ Type, name string, enabled bool, interval time.Duration, settings Settings,
	lastSync *SyncReport, exclusions []Exclusion, createdAt, updatedAt time.Time) *Source {
	return &Source{
		id:           id,
		typ:          typ,
		name:         name,
		enabled:      enabled,
		syncInterval: interval,
		settings:     settings,
		lastSync:     lastSync,
		exclusions:   exclusions,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
	}
}

// ID returns the source's identifier.
func (s *Source) ID() ID { return s.id }

// Type returns the provider behind the source, e.g. "steam".
func (s *Source) Type() Type { return s.typ }

// Name returns the name the user gave the source.
func (s *Source) Name() string { return s.name }

// Enabled reports whether the source takes part in scans.
func (s *Source) Enabled() bool { return s.enabled }

// SyncInterval returns how often the source is scanned automatically; zero means never.
func (s *Source) SyncInterval() time.Duration { return s.syncInterval }

// CreatedAt returns when the source was added.
func (s *Source) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns when the source's configuration was last changed.
func (s *Source) UpdatedAt() time.Time { return s.updatedAt }

// Settings returns a copy of the raw settings (secrets included).
func (s *Source) Settings() Settings {
	out := make(Settings, len(s.settings))
	maps.Copy(out, s.settings)

	return out
}

// LastSync returns the last scan report, or nil if the source was never scanned.
func (s *Source) LastSync() *SyncReport { return s.lastSync }

// Reconfigure updates the source. Secrets sent as SecretPlaceholder are kept.
func (s *Source) Reconfigure(d TypeDescriptor, cfg Config, now time.Time) error {
	cfg.Settings = d.MergeSettings(s.settings, cfg.Settings)

	cfg, err := cfg.validate(d)
	if err != nil {
		return err
	}

	s.name, s.enabled, s.syncInterval, s.settings = cfg.Name, cfg.Enabled, cfg.SyncInterval, cfg.Settings
	s.updatedAt = now

	return nil
}

// UpdateState stores the state fields an integration changed itself (a rotated token). Other
// settings are left alone. It reports whether anything changed.
func (s *Source) UpdateState(d TypeDescriptor, settings Settings, now time.Time) bool {
	next, prev := d.Fields.State(settings), d.Fields.State(s.settings)
	if maps.Equal(next, prev) {
		return false
	}

	merged := Settings{}

	for k, v := range s.settings {
		if _, isState := prev[k]; !isState {
			merged[k] = v
		}
	}

	maps.Copy(merged, next)
	s.settings, s.updatedAt = merged, now

	return true
}

// ReplaceSettings stores settings an integration prepared from what the user typed (e.g. a
// one-time code exchanged for a session). Validation already happened in New or Reconfigure.
func (s *Source) ReplaceSettings(d TypeDescriptor, settings Settings, now time.Time) {
	out := Settings{}

	for _, f := range d.Fields {
		if v := settings[f.Key]; v != "" {
			out[f.Key] = v
		}
	}

	s.settings, s.updatedAt = out, now
}

// RecordSync stores the outcome of a scan.
func (s *Source) RecordSync(r SyncReport) {
	s.lastSync = &r
}

// Exclusions returns the items the user removed from this source, newest first.
func (s *Source) Exclusions() []Exclusion { return slices.Clone(s.exclusions) }

// Excludes reports whether the item with this external id was removed by the user.
func (s *Source) Excludes(externalID string) bool {
	return slices.ContainsFunc(s.exclusions, func(e Exclusion) bool { return e.ExternalID == externalID })
}

// Exclude records that the user removed an item. Removing it again keeps the first entry.
func (s *Source) Exclude(e Exclusion) error {
	if e.ExternalID == "" {
		return invalid("an item to remove needs its id at the source")
	}

	if s.Excludes(e.ExternalID) {
		return nil
	}

	s.exclusions = append(s.exclusions, e)
	slices.SortStableFunc(s.exclusions, func(a, b Exclusion) int { return b.At.Compare(a.At) })

	return nil
}

// Include takes an item off the removed list, so the next scan imports it again. It reports
// whether the item was listed.
func (s *Source) Include(externalID string) bool {
	n := len(s.exclusions)
	s.exclusions = slices.DeleteFunc(s.exclusions, func(e Exclusion) bool { return e.ExternalID == externalID })

	return len(s.exclusions) < n
}

// Repository is the persistence port for sources.
type Repository interface {
	// List returns every source.
	List(ctx context.Context) ([]*Source, error)

	// Get returns one source, or ErrNotFound.
	Get(ctx context.Context, id ID) (*Source, error)

	// Save creates or updates a source.
	Save(ctx context.Context, s *Source) error

	// Delete removes a source (its copies are handled by the caller).
	Delete(ctx context.Context, id ID) error
}
