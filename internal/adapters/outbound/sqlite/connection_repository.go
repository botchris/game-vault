package sqlite

import (
	"context"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
)

// ConnectionRepository implements notify.Connections. Each connection is one JSON document.
type ConnectionRepository struct {
	docs docTable[connection.ID, *connection.Connection]
}

var _ notify.Connections = (*ConnectionRepository)(nil)

// NewConnectionRepository returns the repository backed by db.
func NewConnectionRepository(db *DB) *ConnectionRepository {
	return &ConnectionRepository{docs: docTable[connection.ID, *connection.Connection]{
		db:       db,
		table:    "connections",
		noun:     "connection",
		encode:   encodeConnection,
		decode:   decodeConnection,
		notFound: connection.ErrNotFound,
	}}
}

// List returns every connection, by name.
func (r *ConnectionRepository) List(ctx context.Context) ([]*connection.Connection, error) {
	return r.docs.list(ctx, `ORDER BY json_extract(doc, '$.name') COLLATE NOCASE, id`)
}

// Get returns the connection with the given id.
func (r *ConnectionRepository) Get(ctx context.Context, id connection.ID) (*connection.Connection, error) {
	return r.docs.get(ctx, id)
}

// Save inserts or updates a connection.
func (r *ConnectionRepository) Save(ctx context.Context, c *connection.Connection) error {
	return r.docs.save(ctx, c.ID(), c)
}

// Delete removes a connection.
func (r *ConnectionRepository) Delete(ctx context.Context, id connection.ID) error {
	return r.docs.remove(ctx, id)
}
