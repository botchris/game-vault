package media

import (
	"sync"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// missTTL is how long a provider's "no cover" for an edition is trusted when the edition could not
// be marked missing (another provider asked to slow down).
const missTTL = 24 * time.Hour

// missKey names one provider's answer for one edition.
type missKey struct {
	provider provider.ID
	game     game.ID
	system   string
}

// misses remembers, in memory, which providers answered "no cover" for an edition whose lookup ended
// without a "no cover found" marker because another provider asked to slow down (TheGamesDB's
// allowance used up, for example). Without it, every view would ask the providers that already said
// no (Steam's title search, IGDB) again while the limited one recovers.
type misses struct {
	mu      sync.Mutex
	entries map[missKey]time.Time
}

// fresh returns the providers that answered "no cover" for the edition less than missTTL ago.
func (m *misses) fresh(id game.ID, system string, now time.Time) []provider.ID {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []provider.ID

	for k, at := range m.entries {
		if k.game == id && k.system == system && now.Sub(at) < missTTL {
			out = append(out, k.provider)
		}
	}

	return out
}

// remember records that the providers answered "no cover" for the edition, dropping expired entries.
func (m *misses) remember(id game.ID, system string, providers []provider.ID, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.entries == nil {
		m.entries = map[missKey]time.Time{}
	}

	for k, at := range m.entries {
		if now.Sub(at) >= missTTL {
			delete(m.entries, k)
		}
	}

	for _, p := range providers {
		m.entries[missKey{
			provider: p,
			game:     id,
			system:   system,
		}] = now
	}
}

// forget drops what is remembered for a game (every edition when system is "*"), or for every game
// when id is empty.
func (m *misses) forget(id game.ID, system string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k := range m.entries {
		if id == "" || (k.game == id && (system == "*" || k.system == system)) {
			delete(m.entries, k)
		}
	}
}
