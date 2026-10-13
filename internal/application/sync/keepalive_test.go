package sync_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// rotating is a source whose session token changes on every use, like Ubisoft's.
type rotating struct{ calls int }

func (r *rotating) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "rot",
		Name: "Rotating",
		Fields: []schema.Field{
			{
				Key:      "token",
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:  "session",
				Kind: schema.FieldState,
			},
		},
	}
}

func (r *rotating) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, nil
}

func (r *rotating) Test(context.Context, schema.Settings) error { return nil }

func (r *rotating) KeepAliveInterval() time.Duration { return time.Hour }

func (r *rotating) KeepAlive(_ context.Context, s schema.Settings) error {
	r.calls++
	s["session"] = "renewed-" + string(rune('0'+r.calls))

	return nil
}

func TestKeepAliveSavesRenewedSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	if err != nil {
		t.Fatal(err)
	}

	sources := sqlite.NewSourceRepository(db)
	p := &rotating{}
	svc := sync.NewService(sources, sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
		[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	v, err := svc.Create(ctx, "rot", source.Config{
		Enabled:  true,
		Settings: schema.Settings{"token": "t"},
	})
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

func TestKeepAliveDelay_variesByThirtyPercent(t *testing.T) {
	svc := sync.NewService(nil, nil, nil, nil, nil)

	for draw, want := range map[float64]time.Duration{0: 42 * time.Minute, 0.5: time.Hour, 1: 78 * time.Minute} {
		svc.SetRhythm(unattended.Rhythm{Random: func() float64 { return draw }})

		if d := svc.KeepAliveDelay(time.Hour); d != want {
			t.Fatalf("draw %v: want %v, got %v", draw, want, d)
		}
	}
}

// counting is a source that counts its scans.
type counting struct{ scans int }

func (c *counting) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "cnt",
		Name: "Counting",
	}
}

func (c *counting) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	c.scans++
	return nil, nil, nil
}

func (c *counting) Test(context.Context, schema.Settings) error { return nil }

func TestSchedulerScansOnceAndWaitsForTheVariedInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	if err != nil {
		t.Fatal(err)
	}

	p := &counting{}

	svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
		[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	if _, err := svc.Create(ctx, "cnt", source.Config{
		Enabled:      true,
		SyncInterval: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	go svc.RunScheduler(ctx, 5*time.Millisecond, 0)

	time.Sleep(150 * time.Millisecond)

	if p.scans != 1 {
		t.Fatalf("a never-scanned source is scanned once, then waits about an hour: %d scans", p.scans)
	}
}

func TestScanDelay_variesByTenPercent(t *testing.T) {
	svc := sync.NewService(nil, nil, nil, nil, nil)

	for draw, want := range map[float64]time.Duration{0: 21*time.Hour + 36*time.Minute, 1: 26*time.Hour + 24*time.Minute} {
		svc.SetRhythm(unattended.Rhythm{Random: func() float64 { return draw }})

		if d := svc.ScanDelay(24 * time.Hour); d != want {
			t.Fatalf("draw %v: want %v, got %v", draw, want, d)
		}
	}
}
