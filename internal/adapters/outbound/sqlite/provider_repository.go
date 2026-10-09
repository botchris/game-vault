package sqlite

import (
	"context"
	"fmt"

	"gamevault/internal/domain/provider"
)

// ProviderRepository implements provider.Repository. Each provider's configuration is one JSON
// document.
type ProviderRepository struct{ db *DB }

// NewProviderRepository returns the repository backed by db.
func NewProviderRepository(db *DB) *ProviderRepository { return &ProviderRepository{db: db} }

// List returns the stored providers of a kind, in chain order.
func (r *ProviderRepository) List(ctx context.Context, kind provider.Kind) ([]*provider.Provider, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, doc FROM providers WHERE json_extract(doc, '$.kind') = ?
		 ORDER BY json_extract(doc, '$.priority'), id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*provider.Provider

	for rows.Next() {
		var (
			id  provider.ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		p, err := decodeProvider(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading provider %s: %w", id, err)
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// Save inserts or updates a provider.
func (r *ProviderRepository) Save(ctx context.Context, p *provider.Provider) error {
	raw, err := encodeProvider(p)
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO providers (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, p.ID(), raw)

	return err
}
