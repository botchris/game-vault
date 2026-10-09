// Package sqlite implements the persistence ports on top of SQLite (pure Go driver, no cgo).
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	// ExecContext runs a statement that returns no rows.
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)

	// QueryContext runs a query that returns rows.
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// QueryRowContext runs a query that returns at most one row.
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

// Open opens (creating if needed) the database at path and applies pending migrations. When an
// existing database has pending migrations and backupDir is set, a copy is written there first
// (pre-migration-<first pending>.db), so an upgrade that goes wrong can be undone.
func Open(ctx context.Context, path, backupDir string) (*DB, error) {
	return openUpTo(ctx, path, backupDir, "")
}

// openUpTo is Open applying only the migrations up to last (all when last is empty), so tests can
// build a database as an older version left it.
func openUpTo(ctx context.Context, path, backupDir, last string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serializes writes; plenty for a single-user app and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	d := &DB{sql: db}
	if err := d.migrate(ctx, backupDir, last); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return d, nil
}

// Close closes the underlying database handle.
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

// migrationNumber is the number part of a migration version: "0009_documents" is "0009".
func migrationNumber(version string) string {
	n, _, _ := strings.Cut(version, "_")
	return n
}

func (d *DB) migrate(ctx context.Context, backupDir, last string) error {
	if _, err := d.sql.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}

	sort.Strings(files)

	var applied int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		return err
	}

	backedUp := false

	for _, f := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(f, "migrations/"), ".sql")
		if last != "" && migrationNumber(version) > last {
			break
		}

		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
			return err
		}

		if n > 0 {
			continue
		}

		// An existing database is copied once, before its first pending migration.
		if applied > 0 && backupDir != "" && !backedUp {
			if err := d.backupBeforeMigrating(ctx, backupDir, migrationNumber(version)); err != nil {
				return err
			}

			backedUp = true
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

// backupBeforeMigrating writes pre-migration-<number>.db into dir, or a timestamped name when that
// file exists already (a pre-migration copy that was restored and is being migrated again).
func (d *DB) backupBeforeMigrating(ctx context.Context, dir, number string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s for the pre-migration backup: %w", dir, err)
	}

	path := filepath.Join(dir, "pre-migration-"+number+".db")
	if _, err := os.Stat(path); err == nil {
		path = filepath.Join(dir, fmt.Sprintf("pre-migration-%s-%s.db", number, time.Now().UTC().Format("20060102-150405")))
	}

	if err := d.BackupTo(ctx, path); err != nil {
		return fmt.Errorf("backing up the database to %s before migrating it: %w", path, err)
	}

	return nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
