// Package sqlite implements the persistence ports on top of SQLite (pure Go driver, no cgo).
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB wraps the database handle and implements port.TxManager.
type DB struct {
	sql *sql.DB
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

// Open opens (creating if needed) the database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serialises writes; plenty for a single-user app and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	d := &DB{sql: db}
	if err := d.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// conn returns the transaction stored in ctx, or the plain handle.
func (d *DB) conn(ctx context.Context) querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return d.sql
}

// WithinTx runs fn in a transaction. Nested calls reuse the outer transaction.
func (d *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// BackupTo writes a consistent copy of the database to path (implements system.DatabaseBackup).
func (d *DB) BackupTo(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("backup %s already exists", path)
	}
	_, err := d.sql.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(f, "migrations/"), ".sql")
		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrations.ReadFile(f)
		if err != nil {
			return err
		}
		err = d.WithinTx(ctx, func(ctx context.Context) error {
			if _, err := d.conn(ctx).ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", version, err)
			}
			_, err := d.conn(ctx).ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, formatTime(time.Now()))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
