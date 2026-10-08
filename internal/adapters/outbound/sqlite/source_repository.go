package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gamevault/internal/domain/source"
)

// SourceRepository implements source.Repository.
type SourceRepository struct{ db *DB }

// NewSourceRepository returns the repository backed by db.
func NewSourceRepository(db *DB) *SourceRepository { return &SourceRepository{db: db} }

const sourceCols = `id, type, name, enabled, sync_interval_seconds, settings, last_sync, created_at, updated_at`

// syncReportJSON is the stored form of source.SyncReport.
type syncReportJSON struct {
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	Err             string    `json:"error,omitempty"`
	Fetched         int       `json:"fetched"`
	CopiesAdded     int       `json:"copiesAdded"`
	CopiesUpdated   int       `json:"copiesUpdated"`
	CopiesUnchanged int       `json:"copiesUnchanged"`
	GamesCreated    int       `json:"gamesCreated"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// List returns every source.
func (r *SourceRepository) List(ctx context.Context) ([]*source.Source, error) {
	rows, err := r.db.conn(ctx).QueryContext(ctx, `SELECT `+sourceCols+` FROM sources ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*source.Source

	for rows.Next() {
		s, err := scanSource(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, rows.Err()
}

// Get returns the source with the given id.
func (r *SourceRepository) Get(ctx context.Context, id source.ID) (*source.Source, error) {
	s, err := scanSource(r.db.conn(ctx).QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, source.ErrNotFound
	}

	return s, err
}

// Save inserts or updates a source.
func (r *SourceRepository) Save(ctx context.Context, s *source.Source) error {
	settings, err := json.Marshal(s.Settings())
	if err != nil {
		return err
	}

	var lastSync any

	if rep := s.LastSync(); rep != nil {
		b, err := json.Marshal(syncReportJSON(*rep))
		if err != nil {
			return err
		}

		lastSync = string(b)
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO sources (`+sourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, enabled = excluded.enabled,
		  sync_interval_seconds = excluded.sync_interval_seconds, settings = excluded.settings,
		  last_sync = excluded.last_sync, updated_at = excluded.updated_at`,
		s.ID(), s.Type(), s.Name(), s.Enabled(), int64(s.SyncInterval()/time.Second), string(settings), lastSync,
		formatTime(s.CreatedAt()), formatTime(s.UpdatedAt()))

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

func scanSource(sc interface{ Scan(...any) error }) (*source.Source, error) {
	var (
		id                     source.ID
		typ                    source.Type
		name, settingsJSON     string
		enabled                bool
		intervalSeconds        int64
		lastSyncJSON           sql.NullString
		createdStr, updatedStr string
	)
	if err := sc.Scan(&id, &typ, &name, &enabled, &intervalSeconds, &settingsJSON, &lastSyncJSON, &createdStr, &updatedStr); err != nil {
		return nil, err
	}

	settings := source.Settings{}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return nil, err
	}

	var lastSync *source.SyncReport

	if lastSyncJSON.Valid {
		var rep syncReportJSON
		if err := json.Unmarshal([]byte(lastSyncJSON.String), &rep); err != nil {
			return nil, err
		}

		sr := source.SyncReport(rep)
		lastSync = &sr
	}

	return source.Rehydrate(id, typ, name, enabled, time.Duration(intervalSeconds)*time.Second, settings, lastSync,
		parseTime(createdStr), parseTime(updatedStr)), nil
}
