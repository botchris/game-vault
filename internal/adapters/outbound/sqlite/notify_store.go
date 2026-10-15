package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// settingSeeded is the settings row that records the watcher ran once; settingNotifications holds
// the notification settings document.
const (
	settingSeeded        = "notifications.seeded"
	settingNotifications = "notifications"
)

// NotifyStore implements the notify package's stores: the outbox, the memory of notified issues
// and key deadlines, and the notification settings.
type NotifyStore struct{ db *DB }

var (
	_ notify.Outbox        = (*NotifyStore)(nil)
	_ notify.IssueMemory   = (*NotifyStore)(nil)
	_ notify.KeyMemory     = (*NotifyStore)(nil)
	_ notify.SettingsStore = (*NotifyStore)(nil)
)

// NewNotifyStore returns the store backed by db.
func NewNotifyStore(db *DB) *NotifyStore { return &NotifyStore{db: db} }

// notificationsDoc is the stored form of the notification settings.
type notificationsDoc struct {
	V       int    `json:"v"`
	Address string `json:"address"`
}

// Enqueue implements notify.Outbox.
func (s *NotifyStore) Enqueue(ctx context.Context, ds []notify.Delivery) error {
	return s.db.WithinTx(ctx, func(ctx context.Context) error { return s.enqueue(ctx, ds) })
}

func (s *NotifyStore) enqueue(ctx context.Context, ds []notify.Delivery) error {
	for _, d := range ds {
		raw, err := notify.EncodeEvent(d.Event)
		if err != nil {
			return err
		}

		_, err = s.db.conn(ctx).ExecContext(ctx, `INSERT INTO notification_outbox
			(id, connection_id, event, state, attempts, next_at, last_error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.Connection, raw, d.State, d.Attempts, sortableTime(d.NextAt), d.LastError,
			sortableTime(d.CreatedAt), sortableTime(d.UpdatedAt))
		if err != nil {
			return err
		}
	}

	return nil
}

// sortableTime writes t as UTC RFC 3339 with a fixed nine-digit fraction. The outbox compares and
// orders its times as text, and formatTime drops trailing zeros ("08:00:00Z" sorts after
// "08:00:00.5Z"), so these columns need a fixed width; parseTime reads both.
func sortableTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }

const deliveryColumns = `id, connection_id, event, state, attempts, next_at, last_error, created_at, updated_at`

// Due implements notify.Outbox.
func (s *NotifyStore) Due(ctx context.Context, now time.Time) ([]notify.Delivery, error) {
	return s.deliveries(ctx, `SELECT `+deliveryColumns+` FROM notification_outbox
		WHERE state = ? AND next_at <= ? ORDER BY next_at, created_at, id`, notify.StateQueued, sortableTime(now))
}

// MarkSent implements notify.Outbox.
func (s *NotifyStore) MarkSent(ctx context.Context, id string, at time.Time) error {
	return s.update(ctx, id, `state = ?, last_error = '', updated_at = ?`, notify.StateSent, sortableTime(at))
}

// MarkRetry implements notify.Outbox.
func (s *NotifyStore) MarkRetry(ctx context.Context, id string, attempts int, next time.Time, reason string, at time.Time) error {
	return s.update(ctx, id, `attempts = ?, next_at = ?, last_error = ?, updated_at = ?`,
		attempts, sortableTime(next), reason, sortableTime(at))
}

// MarkFailed implements notify.Outbox.
func (s *NotifyStore) MarkFailed(ctx context.Context, id string, reason string, at time.Time) error {
	return s.update(ctx, id, `state = ?, last_error = ?, updated_at = ?`, notify.StateFailed, reason, sortableTime(at))
}

// MarkDropped implements notify.Outbox.
func (s *NotifyStore) MarkDropped(ctx context.Context, id string, at time.Time) error {
	return s.update(ctx, id, `state = ?, updated_at = ?`, notify.StateDropped, sortableTime(at))
}

// update sets columns of one delivery; a delivery deleted meanwhile (its connection was) is not an
// error, as there is nothing left to record.
func (s *NotifyStore) update(ctx context.Context, id, set string, args ...any) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `UPDATE notification_outbox SET `+set+` WHERE id = ?`, append(args, id)...)

	return err
}

// Recent implements notify.Outbox.
func (s *NotifyStore) Recent(ctx context.Context, id connection.ID, limit int) ([]notify.Delivery, error) {
	return s.deliveries(ctx, `SELECT `+deliveryColumns+` FROM notification_outbox
		WHERE connection_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, id, limit)
}

// DeleteFor implements notify.Outbox.
func (s *NotifyStore) DeleteFor(ctx context.Context, id connection.ID) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `DELETE FROM notification_outbox WHERE connection_id = ?`, id)

	return err
}

// Prune implements notify.Outbox; queued deliveries are never pruned.
func (s *NotifyStore) Prune(ctx context.Context, before time.Time) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `DELETE FROM notification_outbox WHERE state <> ? AND updated_at < ?`,
		notify.StateQueued, sortableTime(before))

	return err
}

func (s *NotifyStore) deliveries(ctx context.Context, query string, args ...any) ([]notify.Delivery, error) {
	rows, err := s.db.conn(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []notify.Delivery

	for rows.Next() {
		var (
			d                  notify.Delivery
			raw, next, created string
			updated            string
		)

		if err := rows.Scan(&d.ID, &d.Connection, &raw, &d.State, &d.Attempts, &next, &d.LastError, &created, &updated); err != nil {
			return nil, err
		}

		if d.Event, err = notify.DecodeEvent(raw); err != nil {
			return nil, fmt.Errorf("reading delivery %s: %w", d.ID, err)
		}

		d.NextAt = parseTime(next)
		d.CreatedAt = parseTime(created)
		d.UpdatedAt = parseTime(updated)
		out = append(out, d)
	}

	return out, rows.Err()
}

// Notified implements notify.IssueMemory.
func (s *NotifyStore) Notified(ctx context.Context) (map[string]event.Issue, error) {
	rows, err := s.db.conn(ctx).QueryContext(ctx, `SELECT key, issue, since FROM notified_issues`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]event.Issue{}

	for rows.Next() {
		var key, raw, since string
		if err := rows.Scan(&key, &raw, &since); err != nil {
			return nil, err
		}

		var issue event.Issue
		if err := json.Unmarshal([]byte(raw), &issue); err != nil {
			return nil, fmt.Errorf("reading notified issue %s: %w", key, err)
		}

		out[key] = issue
	}

	return out, rows.Err()
}

// SaveNotified implements notify.IssueMemory: the issues replace the remembered ones.
func (s *NotifyStore) SaveNotified(ctx context.Context, issues map[string]event.Issue) error {
	return s.db.WithinTx(ctx, func(ctx context.Context) error { return s.saveNotified(ctx, issues) })
}

func (s *NotifyStore) saveNotified(ctx context.Context, issues map[string]event.Issue) error {
	c := s.db.conn(ctx)

	if _, err := c.ExecContext(ctx, `DELETE FROM notified_issues`); err != nil {
		return err
	}

	for key, issue := range issues {
		raw, err := json.Marshal(issue)
		if err != nil {
			return err
		}

		if _, err := c.ExecContext(ctx, `INSERT INTO notified_issues (key, issue, since) VALUES (?, ?, ?)`,
			key, string(raw), formatTime(issue.Since)); err != nil {
			return err
		}
	}

	return nil
}

// Seeded implements notify.IssueMemory.
func (s *NotifyStore) Seeded(ctx context.Context) (bool, error) {
	_, ok, err := s.db.readSetting(ctx, settingSeeded)

	return ok, err
}

// MarkSeeded implements notify.IssueMemory.
func (s *NotifyStore) MarkSeeded(ctx context.Context) error {
	return s.db.writeSetting(ctx, settingSeeded, "true")
}

// KeyThresholds implements notify.KeyMemory.
func (s *NotifyStore) KeyThresholds(ctx context.Context) (map[game.ID][]int, error) {
	rows, err := s.db.conn(ctx).QueryContext(ctx, `SELECT copy_id, threshold FROM notified_keys ORDER BY copy_id, threshold DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[game.ID][]int{}

	for rows.Next() {
		var (
			id game.ID
			t  int
		)
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}

		out[id] = append(out[id], t)
	}

	return out, rows.Err()
}

// RememberKey implements notify.KeyMemory.
func (s *NotifyStore) RememberKey(ctx context.Context, copyID game.ID, threshold int, at time.Time) error {
	_, err := s.db.conn(ctx).ExecContext(ctx, `INSERT INTO notified_keys (copy_id, threshold, at) VALUES (?, ?, ?)
		ON CONFLICT(copy_id, threshold) DO NOTHING`, copyID, threshold, sortableTime(at))

	return err
}

// ForgetKeys implements notify.KeyMemory.
func (s *NotifyStore) ForgetKeys(ctx context.Context, keep []game.ID) error {
	if len(keep) == 0 {
		_, err := s.db.conn(ctx).ExecContext(ctx, `DELETE FROM notified_keys`)

		return err
	}

	// One placeholder per kept copy would hit SQLite's variable limit on a very large catalog, so
	// the copies to keep go in as one JSON array.
	raw, err := json.Marshal(keep)
	if err != nil {
		return err
	}

	_, err = s.db.conn(ctx).ExecContext(ctx,
		`DELETE FROM notified_keys WHERE copy_id NOT IN (SELECT value FROM json_each(?))`, string(raw))

	return err
}

// Address implements notify.SettingsStore.
func (s *NotifyStore) Address(ctx context.Context) (string, error) {
	var doc notificationsDoc
	if _, err := s.db.getSetting(ctx, settingNotifications, &doc); err != nil {
		return "", err
	}

	return strings.TrimSpace(doc.Address), nil
}

// SaveAddress implements notify.SettingsStore.
func (s *NotifyStore) SaveAddress(ctx context.Context, address string) error {
	return s.db.putSetting(ctx, settingNotifications, notificationsDoc{
		V:       1,
		Address: strings.TrimSpace(address),
	})
}
