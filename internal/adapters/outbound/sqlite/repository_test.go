package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

func openTest(t *testing.T) *DB {
	t.Helper()

	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), "")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })

	return db
}

func TestGameRepositoryRoundTripAndMove(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	repo := NewGameRepository(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	a, _ := game.New("Hades", now)
	a.AddCopy(game.CopyDetails{
		Kind:     game.KindKey,
		Platform: "Steam",
		Key:      "AAAA",
		RedeemBy: "2027-01-01",
	}, now)
	b, _ := game.New("Hades II", now)

	if err := repo.Save(ctx, a); err != nil {
		t.Fatal(err)
	}

	if err := repo.Save(ctx, b); err != nil {
		t.Fatal(err)
	}

	// Move the copy from a to b inside a transaction, like catalog.MoveCopy does.
	err := db.WithinTx(ctx, func(ctx context.Context) error {
		c, err := a.RemoveCopy(a.Copies()[0].ID, now)
		if err != nil {
			return err
		}

		b.AttachCopy(c, now)

		if err := repo.Save(ctx, b); err != nil {
			return err
		}

		return repo.Save(ctx, a)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, b.ID())
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Copies()) != 1 || got.Copies()[0].Key != "AAAA" || got.Copies()[0].RedeemBy != "2027-01-01" {
		t.Fatalf("copy not moved: %+v", got.Copies())
	}

	if got, _ := repo.Get(ctx, a.ID()); len(got.Copies()) != 0 {
		t.Fatalf("copy still in source game")
	}

	if err := repo.Delete(ctx, a.ID()); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(ctx, a.ID()); !errors.Is(err, game.ErrGameNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestSourceRepositoryAndBackup(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	repo := NewSourceRepository(db)
	d := source.TypeDescriptor{
		Type: "steam",
		Name: "Steam",
		Fields: []source.Field{{
			Key:      "api_key",
			Kind:     source.FieldSecret,
			Required: true,
		}},
	}

	s, err := source.New(d, source.Config{
		Enabled:      true,
		SyncInterval: 6 * time.Hour,
		Settings:     source.Settings{"api_key": "secret"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	s.RecordSync(source.SyncReport{
		StartedAt: time.Now(),
		Fetched:   3,
		Warnings:  []string{"w"},
	})

	if err := repo.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, s.ID())
	if err != nil {
		t.Fatal(err)
	}

	if got.Settings()["api_key"] != "secret" || got.SyncInterval() != 6*time.Hour || got.LastSync().Fetched != 3 {
		t.Fatalf("unexpected source %+v", got)
	}

	path := filepath.Join(t.TempDir(), "backup.db")
	if err := db.BackupTo(ctx, path); err != nil {
		t.Fatal(err)
	}

	restored, err := Open(ctx, path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	if list, _ := NewSourceRepository(restored).List(ctx); len(list) != 1 {
		t.Fatalf("backup should contain the source, got %d", len(list))
	}
}
