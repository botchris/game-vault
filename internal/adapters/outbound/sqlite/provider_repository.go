package sqlite

import (
	"context"
	"encoding/json"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ProviderRepository implements provider.Repository.
type ProviderRepository struct{ db *DB }

func NewProviderRepository(db *DB) *ProviderRepository { return &ProviderRepository{db: db} }

func (r *ProviderRepository) List(ctx context.Context, kind provider.Kind) ([]*provider.Provider, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx,
		`SELECT id, kind, enabled, priority, settings, updated_at FROM providers WHERE kind = ? ORDER BY priority, id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*provider.Provider
	for rows.Next() {
		var (
			id           provider.ID
			k            provider.Kind
			enabled      bool
			priority     int
			raw, updated string
		)
		if err := rows.Scan(&id, &k, &enabled, &priority, &raw, &updated); err != nil {
			return nil, err
		}
		settings := schema.Settings{}
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return nil, err
		}
		out = append(out, provider.Rehydrate(id, k, enabled, priority, settings, parseTime(updated)))
	}
	return out, rows.Err()
}

func (r *ProviderRepository) Save(ctx context.Context, p *provider.Provider) error {
	raw, err := json.Marshal(p.Settings())
	if err != nil {
		return err
	}
	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO providers (id, kind, enabled, priority, settings, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, enabled = excluded.enabled, priority = excluded.priority,
		  settings = excluded.settings, updated_at = excluded.updated_at`,
		p.ID(), p.Kind(), p.Enabled(), p.Priority(), string(raw), formatTime(p.UpdatedAt()))
	return err
}
