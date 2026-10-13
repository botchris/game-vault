package sqlite

import (
	"context"

	"gamevault/internal/domain/source"
)

// SourceRepository implements source.Repository. Each source is one JSON document.
type SourceRepository struct {
	docs docTable[source.ID, *source.Source]
}

// NewSourceRepository returns the repository backed by db.
func NewSourceRepository(db *DB) *SourceRepository {
	return &SourceRepository{docs: docTable[source.ID, *source.Source]{
		db:       db,
		table:    "sources",
		noun:     "source",
		encode:   encodeSource,
		decode:   decodeSource,
		notFound: source.ErrNotFound,
	}}
}

// List returns every source, by name.
func (r *SourceRepository) List(ctx context.Context) ([]*source.Source, error) {
	return r.docs.list(ctx, `ORDER BY json_extract(doc, '$.name') COLLATE NOCASE, id`)
}

// Get returns the source with the given id.
func (r *SourceRepository) Get(ctx context.Context, id source.ID) (*source.Source, error) {
	return r.docs.get(ctx, id)
}

// Save inserts or updates a source.
func (r *SourceRepository) Save(ctx context.Context, s *source.Source) error {
	return r.docs.save(ctx, s.ID(), s)
}

// Delete removes a source.
func (r *SourceRepository) Delete(ctx context.Context, id source.ID) error {
	return r.docs.remove(ctx, id)
}
