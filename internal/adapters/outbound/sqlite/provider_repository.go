package sqlite

import (
	"context"

	"gamevault/internal/domain/provider"
)

// ProviderRepository implements provider.Repository. Each provider's configuration is one JSON
// document.
type ProviderRepository struct {
	docs docTable[provider.ID, *provider.Provider]
}

// NewProviderRepository returns the repository backed by db.
func NewProviderRepository(db *DB) *ProviderRepository {
	return &ProviderRepository{docs: docTable[provider.ID, *provider.Provider]{
		db:     db,
		table:  "providers",
		noun:   "provider",
		encode: encodeProvider,
		decode: decodeProvider,
	}}
}

// List returns the stored providers of a kind, in chain order.
func (r *ProviderRepository) List(ctx context.Context, kind provider.Kind) ([]*provider.Provider, error) {
	return r.docs.list(ctx, `WHERE json_extract(doc, '$.kind') = ?
		 ORDER BY json_extract(doc, '$.priority'), id`, kind)
}

// Save inserts or updates a provider.
func (r *ProviderRepository) Save(ctx context.Context, p *provider.Provider) error {
	return r.docs.save(ctx, p.ID(), p)
}
