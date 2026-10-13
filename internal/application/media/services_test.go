package media_test

import (
	"time"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
)

// newServices builds the providers registry over db, with impls registered in that order, and the
// media services over it.
func newServices(db *sqlite.DB, games game.Repository, store media.AssetStore, fetch media.ImageFetcher,
	impls []providers.Impl, opts ...media.Option) *media.Services {
	return newServicesAt(db, time.Now, games, store, fetch, impls, opts...)
}

// newServicesAt is newServices with the registry and the services on the clock now.
func newServicesAt(db *sqlite.DB, now port.Clock, games game.Repository, store media.AssetStore, fetch media.ImageFetcher,
	impls []providers.Impl, opts ...media.Option) *media.Services {
	reg := providers.NewRegistry(sqlite.NewProviderRepository(db), impls, providers.WithClock(now))

	return media.New(games, reg, store, fetch, append([]media.Option{media.WithClock(now)}, opts...)...)
}
