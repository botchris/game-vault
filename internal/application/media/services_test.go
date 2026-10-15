package media_test

import (
	"testing"
	"time"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
)

// newServices builds the providers registry over db, with impls registered in that order, and the
// media services over it.
func newServices(t testing.TB, db *sqlite.DB, games game.Repository, store media.AssetStore, fetch media.ImageFetcher,
	impls []providers.Impl, opts ...media.Option) *media.Services {
	return newServicesAt(t, db, time.Now, games, store, fetch, impls, opts...)
}

// newServicesAt is newServices with the registry and the services on the clock now. The test's
// cleanup waits for the sheets' background downloads, which would otherwise still be writing into
// the test's temporary game data when it is removed.
func newServicesAt(t testing.TB, db *sqlite.DB, now port.Clock, games game.Repository, store media.AssetStore, fetch media.ImageFetcher,
	impls []providers.Impl, opts ...media.Option) *media.Services {
	reg := providers.NewRegistry(sqlite.NewProviderRepository(db), impls, providers.WithClock(now))
	svc := media.New(games, reg, store, fetch, append([]media.Option{media.WithClock(now)}, opts...)...)
	t.Cleanup(svc.Assets.Wait)

	return svc
}
