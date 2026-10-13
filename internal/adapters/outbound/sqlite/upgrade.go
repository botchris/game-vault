package sqlite

import (
	"context"
	"encoding/json"

	"gamevault/internal/domain/game"
)

// This file holds what the one-time start-up upgrade (internal/application/upgrade) needs to find
// the rows still stored in an older shape. It goes with the upgrade and the legacy readers.

// OutdatedIDs returns the ids of the games stored in a document version older than the current
// one, in id order. Nothing is decoded, so a database with nothing to upgrade costs one query.
func (r *GameRepository) OutdatedIDs(ctx context.Context) ([]game.ID, error) {
	rows, err := r.docs.db.conn(ctx).QueryContext(ctx,
		`SELECT id FROM games WHERE json_extract(doc, '$.v') < ? ORDER BY id`, gameDocVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []game.ID

	for rows.Next() {
		var id game.ID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// AuthOutdated reports whether the security settings are stored in the shape of the versions that
// trusted Tailscale identities: a row without "authentication", which Auth converts when it reads
// it. No row at all is not outdated (the defaults apply).
func (r *SettingsRepository) AuthOutdated(ctx context.Context) (bool, error) {
	raw, ok, err := r.db.readSetting(ctx, keyAuth)
	if err != nil || !ok {
		return false, err
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return false, err
	}

	_, current := fields["authentication"]

	return !current, nil
}
