package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// docTable is a table of JSON documents, (id, doc), each the stored form of one aggregate: games,
// sources and providers. It holds what every such repository does the same way, so they only say
// how to encode and decode their aggregate.
type docTable[ID ~string, T any] struct {
	db *DB

	// table is the table's name; noun names a row in errors ("game").
	table string
	noun  string

	encode func(T) (string, error)
	decode func(ID, string) (T, error)

	// notFound is returned by get and remove when there is no row with the id.
	notFound error
}

// list returns the documents the clause selects (a WHERE and an ORDER BY), decoded.
func (t docTable[ID, T]) list(ctx context.Context, clause string, args ...any) ([]T, error) {
	rows, err := t.db.conn(ctx).QueryContext(ctx, `SELECT id, doc FROM `+t.table+` `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []T

	for rows.Next() {
		var (
			id  ID
			raw string
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}

		v, err := t.decode(id, raw)
		if err != nil {
			return nil, fmt.Errorf("reading %s %s: %w", t.noun, id, err)
		}

		out = append(out, v)
	}

	return out, rows.Err()
}

// get returns the document with the id, decoded, or notFound.
func (t docTable[ID, T]) get(ctx context.Context, id ID) (T, error) {
	var (
		zero T
		raw  string
	)

	err := t.db.conn(ctx).QueryRowContext(ctx, `SELECT doc FROM `+t.table+` WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, t.notFound
	}

	if err != nil {
		return zero, err
	}

	v, err := t.decode(id, raw)
	if err != nil {
		return zero, fmt.Errorf("reading %s %s: %w", t.noun, id, err)
	}

	return v, nil
}

// save inserts or replaces the document with the id.
func (t docTable[ID, T]) save(ctx context.Context, id ID, v T) error {
	raw, err := t.encode(v)
	if err != nil {
		return err
	}

	_, err = t.db.conn(ctx).ExecContext(ctx,
		`INSERT INTO `+t.table+` (id, doc) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET doc = excluded.doc`, id, raw)

	return err
}

// remove deletes the document with the id, or returns notFound when there is none.
func (t docTable[ID, T]) remove(ctx context.Context, id ID) error {
	res, err := t.db.conn(ctx).ExecContext(ctx, `DELETE FROM `+t.table+` WHERE id = ?`, id)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return t.notFound
	}

	return nil
}

// readSetting returns the value of a row of the settings table, and whether there is one.
func (d *DB) readSetting(ctx context.Context, key string) (string, bool, error) {
	var raw string

	err := d.conn(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, err
	}

	return raw, true, nil
}

// writeSetting inserts or replaces a row of the settings table.
func (d *DB) writeSetting(ctx context.Context, key, value string) error {
	_, err := d.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now()))

	return err
}

// getSetting decodes the JSON row of the settings table into v, and reports whether there is one;
// without one, v is left as it is.
func (d *DB) getSetting(ctx context.Context, key string, v any) (bool, error) {
	raw, ok, err := d.readSetting(ctx, key)
	if err != nil || !ok {
		return false, err
	}

	return true, json.Unmarshal([]byte(raw), v)
}

// putSetting stores v as the JSON row of the settings table.
func (d *DB) putSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return d.writeSetting(ctx, key, string(b))
}
