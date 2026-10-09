package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gamevault/internal/domain/source"
)

// SourceRepository implements source.Repository. Each source is one JSON document.
type SourceRepository struct{ db *DB }

// NewSourceRepository returns the repository backed by db.
func NewSourceRepository(db *DB) *SourceRepository { return &SourceRepository{db: db} }

// List returns every source, by name.
func (r *SourceRepository) List(ctx context.Context) ([]*source.Source, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM sources ORDER BY json_extract(doc, '$.name') COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*source.Source

	for rows.Next() {
		var (
			id  source.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		s, err := decodeSource(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading source %s: %w", id, err)
		}

		out = append(out, s)
	}

	return out, rows.Err()
}

// Get returns the source with the given id.
func (r *SourceRepository) Get(ctx context.Context, id source.ID) (*source.Source, error) {
	var raw string

	err := r.db.conn(ctx).QueryRowContext(ctx, `SELECT doc FROM sources WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, source.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	s, err := decodeSource(id, raw)
	if err != nil {
		return nil, fmt.Errorf("reading source %s: %w", id, err)
	}

	return s, nil
}

// Save inserts or updates a source.
func (r *SourceRepository) Save(ctx context.Context, s *source.Source) error {
	raw, err := encodeSource(s)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO sources (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, s.ID(), raw)

	return err
}

// Delete removes a source.
func (r *SourceRepository) Delete(ctx context.Context, id source.ID) error {
	res, err := r.db.conn(ctx).ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return source.ErrNotFound
	}

	return nil
}
