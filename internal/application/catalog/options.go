package catalog

import (
	"context"

	"gamevault/internal/application/events"
	"gamevault/internal/application/port"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

// Option configures NewService.
type Option func(*Service)

// WithClock sets the clock that dates changes (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithCoverCache sets the cache whose covers changes drop (none by default: NoCoverCache).
func WithCoverCache(covers CoverCache) Option { return func(s *Service) { s.covers = covers } }

// WithPhotoFiles sets the store that tells whether a photo to attach was uploaded. Without one,
// attached photos are not checked.
func WithPhotoFiles(photos PhotoFiles) Option { return func(s *Service) { s.photos = photos } }

// WithFields sets the custom field definitions. Without them, no custom fields are defined.
func WithFields(fields field.Repository) Option { return func(s *Service) { s.fields = fields } }

// WithEvents sets the publisher told about the games added by hand (none by default: events.Nop).
func WithEvents(p events.Publisher) Option { return func(s *Service) { s.events = p } }

// anyPhoto is the PhotoFiles of a service built without one: every photo counts as uploaded.
type anyPhoto struct{}

func (anyPhoto) Has(game.PhotoID) bool { return true }

// noFields is the field.Repository of a service built without one: no custom fields are defined.
type noFields struct{}

func (noFields) Fields(context.Context) (*field.Set, error) { return field.NewSet(nil), nil }

func (noFields) SaveFields(context.Context, *field.Set) error { return nil }
