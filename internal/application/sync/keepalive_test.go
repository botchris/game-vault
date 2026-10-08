package sync_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// rotating is a source whose session token changes on every use, like Ubisoft's.
type rotating struct{ calls int }

func (r *rotating) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{Type: "rot", Name: "Rotating", Fields: []source.Field{
		{Key: "token", Kind: source.FieldSecret, Required: true},
		{Key: "session", Kind: source.FieldState},
	}}
}

func (r *rotating) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, nil
}

func (r *rotating) KeepAliveInterval() time.Duration { return time.Hour }

func (r *rotating) KeepAlive(_ context.Context, s source.Settings) error {
	r.calls++
	s["session"] = "renewed-" + string(rune('0'+r.calls))

	return nil
}

func TestKeepAliveSavesRenewedSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"))
	if err != nil {
		t.Fatal(err)
	}

	sources := sqlite.NewSourceRepository(db)
	p := &rotating{}
	svc := sync.NewService(sources, sqlite.NewGameRepository(db), db, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

	v, err := svc.Create(ctx, "rot", source.Config{Enabled: true, Settings: source.Settings{"token": "t"}})
	if err != nil {
		t.Fatal(err)
	}

	go svc.RunKeepAlive(ctx, 10*time.Millisecond, 0)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		src, err := sources.Get(ctx, v.ID())
		if err == nil && src.Settings()["session"] == "renewed-1" {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	src, _ := sources.Get(ctx, v.ID())
	if got := src.Settings()["session"]; got != "renewed-1" {
		t.Fatalf("the renewed session should be saved, got %q", got)
	}

	time.Sleep(50 * time.Millisecond)

	if p.calls != 1 {
		t.Fatalf("keep-alive should wait for its interval, called %d times", p.calls)
	}
}

func TestJitteredStaysWithinBounds(t *testing.T) {
	seen := map[time.Duration]bool{}

	for range 1000 {
		d := sync.Jittered(time.Hour)
		if d < 42*time.Minute || d > 78*time.Minute {
			t.Fatalf("out of ±30%%: %v", d)
		}

		seen[d.Round(time.Minute)] = true
	}

	if len(seen) < 20 {
		t.Fatalf("not random enough: %d distinct values", len(seen))
	}
}

// counting is a source that counts its scans.
type counting struct{ scans int }

func (c *counting) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{Type: "cnt", Name: "Counting"}
}

func (c *counting) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	c.scans++
	return nil, nil, nil
}

func TestSchedulerScansOnceAndWaitsForTheVariedInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"))
	if err != nil {
		t.Fatal(err)
	}

	p := &counting{}

	svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), db, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

	if _, err := svc.Create(ctx, "cnt", source.Config{Enabled: true, SyncInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	go svc.RunScheduler(ctx, 5*time.Millisecond, 0)

	time.Sleep(150 * time.Millisecond)

	if p.scans != 1 {
		t.Fatalf("a never-scanned source is scanned once, then waits about an hour: %d scans", p.scans)
	}
}

func TestVaryForScans(t *testing.T) {
	for range 1000 {
		if d := sync.Vary(24*time.Hour, 0.1); d < 21*time.Hour+36*time.Minute || d > 26*time.Hour+24*time.Minute {
			t.Fatalf("out of ±10%%: %v", d)
		}
	}
}
