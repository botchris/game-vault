package sync

import (
	"context"

	"gamevault/internal/domain/source"
)

// Jittered is exported for tests.
var Jittered = jittered

// Vary is exported for tests.
var Vary = vary

// KeepAliveNow runs one keep-alive of the source at once, for tests.
func (s *Service) KeepAliveNow(ctx context.Context, id source.ID) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return
	}

	if k, ok := s.providers[src.Type()].(KeepAliver); ok {
		s.keepAlive(ctx, id, k)
	}
}
