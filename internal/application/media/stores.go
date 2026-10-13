package media

import (
	"context"
	"slices"
	"strings"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
)

// minSearchLen is the shortest text a store's catalog is searched by: a single character matches a
// large part of any catalog and only spends a request.
const minSearchLen = 2

// SourceStores is the port that names the stores the sources link games to (the sync service).
type SourceStores interface {
	// LinkStores returns the stores the sources link games to.
	LinkStores() []game.Store
}

// Stores knows the stores games can be linked to, and searches their catalogs so the user can link
// a game to one.
type Stores struct {
	stores    []LinkStore
	searchers []LinkSearcher
	sources   SourceStores
}

// NewStores builds the stores service over the providers in reg that read store links, and the
// stores sources link games to.
func NewStores(reg *providers.Registry, sources SourceStores) *Stores {
	stores, searchers := linkStoresOf(reg)

	return &Stores{
		stores:    stores,
		searchers: searchers,
		sources:   sources,
	}
}

// LinkStores returns the stores games can be linked to: those the providers read links of (in chain
// order), then those only sources link games to. A store both know is listed once, with the
// providers' name and searchability, and the page URL either gives.
func (s *Stores) LinkStores() []LinkStore {
	out := slices.Clone(s.stores)

	for _, st := range s.sources.LinkStores() {
		i := slices.IndexFunc(out, func(ls LinkStore) bool { return ls.Key == st.Key })
		if i < 0 {
			out = append(out, LinkStore{Store: st})
			continue
		}

		if out[i].PageURL == "" {
			out[i].PageURL = st.PageURL
		}
	}

	return out
}

// SearchLinks searches a store's catalog by title, to link a game to it.
func (s *Stores) SearchLinks(ctx context.Context, store, query string) ([]LinkMatch, error) {
	for _, ls := range s.searchers {
		if ls.LinkStore().Key != store {
			continue
		}

		if len(strings.TrimSpace(query)) < minSearchLen {
			return nil, nil
		}

		matches, err := ls.SearchLinks(ctx, query)
		if err != nil && errkind.Of(err) == errkind.Unknown {
			// A search that fails is the store's trouble (busy, not answering), not Game Vault's.
			return nil, errkind.Wrap(errkind.Unavailable, err)
		}

		return matches, err
	}

	return nil, ErrUnknownStore
}

// linkStoresOf returns the stores whose links the providers in reg read, in default chain order,
// and the providers that can search them (the first one per store).
func linkStoresOf(reg *providers.Registry) (stores []LinkStore, searchers []LinkSearcher) {
	for _, p := range reg.All() {
		linker, ok := p.(StoreLinker)
		if !ok {
			continue
		}

		store := linker.LinkStore()
		i := slices.IndexFunc(stores, func(ls LinkStore) bool { return ls.Key == store.Key })

		if i < 0 {
			stores = append(stores, LinkStore{Store: store})
			i = len(stores) - 1
		}

		if stores[i].PageURL == "" {
			stores[i].PageURL = store.PageURL
		}

		if searcher, ok := p.(LinkSearcher); ok && !stores[i].Searchable {
			stores[i].Searchable = true

			searchers = append(searchers, searcher)
		}
	}

	return stores, searchers
}
