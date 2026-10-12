// Store pages where Game Vault Connector shows what you own. Data, not code: when a store changes
// its pages, fix the patterns and selectors here and the sample page in tests/samples/. A page's
// `path` is a regular expression on the address's path; a store with a `domain` is also every site
// of that domain (language sites); every selector list is tried in order and the first that matches
// wins. Steam, GOG and Humble were checked on the live sites on 2026-10-12 (Humble Choice from a
// member's page).
// Loaded as a content script (a classic script) and imported by the background and the tests, so
// it only adds to globalThis.gvOwnership.
(() => {
  const ns = (globalThis.gvOwnership ??= {});

  ns.STORES = [
    {
      store: 'steam',
      name: 'Steam',
      host: 'store.steampowered.com',
      pages: [
        {
          kind: 'product',
          path: '^/app/(\\d+)',
          id: { fromPath: 1 },
          title: ['#appHubAppName', '.apphub_AppName'],
          anchor: ['#appHubAppName', '.apphub_AppName'],
        },
      ],
    },
    {
      store: 'gog',
      name: 'GOG',
      host: 'www.gog.com',
      pages: [
        {
          kind: 'product',
          path: '^/(?:[a-z]{2}/)?game/[^/?#]+',
          id: { selectors: ['[card-product]'], attr: 'card-product' },
          title: ['h1.productcard-basics__title', '.productcard-basics__title'],
          anchor: ['h1.productcard-basics__title', '.productcard-basics__title'],
        },
      ],
    },
    {
      store: 'humble',
      name: 'Humble Bundle',
      host: 'www.humblebundle.com',
      // Humble serves its translations on language sites (es.humblebundle.com…).
      domain: 'humblebundle.com',
      pages: [
        {
          kind: 'product',
          path: '^/store/[^/?#]+/?$',
          title: ['h1.human_name-view', '.product-header-view h1'],
          anchor: ['h1.human_name-view', '.product-header-view h1'],
        },
        {
          kind: 'bundle',
          path: '^/games/[^/?#]+',
          items: { selectors: ['.tier-item-view'], title: ['.item-title', '.item-title-text'] },
          summary: ['.bundle-logo-text', '.heading-medium', 'h1'],
        },
        {
          // A member's Humble Choice month (the month is in the address's #fragment).
          kind: 'bundle',
          path: '^/membership/home/?$',
          items: { selectors: ['.content-choice'], title: ['.content-choice-title'] },
          summary: ['.content-choices-title', '.subhub-main-title'],
        },
      ],
    },
    {
      store: 'fanatical',
      name: 'Fanatical',
      host: 'www.fanatical.com',
      pages: [
        {
          kind: 'product',
          path: '^/[a-z]{2}(?:-[a-z]{2})?/game/[^/?#]+',
          title: ['h1.product-name', 'main h1'],
          anchor: ['h1.product-name', 'main h1'],
        },
        {
          kind: 'bundle',
          path: '^/[a-z]{2}(?:-[a-z]{2})?/(?:bundle|pick-and-mix)/[^/?#]+',
          items: { selectors: ['.bundle-game-card', '.pnm-product-card'], title: ['.bundle-game-card__title', '.card-title', 'h3', 'h4'] },
          summary: ['main h1', 'h1'],
        },
      ],
    },
  ];

  /** Whether a host name belongs to a store: its host, or any site of its domain when it has one. */
  ns.storeHasHost = (store, host) =>
    host === store.host || (Boolean(store.domain) && (host === store.domain || host.endsWith(`.${store.domain}`)));

  /** The store a page address belongs to (by host name), or null. */
  ns.storeForUrl = (url) => {
    let host;
    try {
      host = new URL(url).hostname;
    } catch {
      return null;
    }
    return ns.STORES.find((s) => ns.storeHasHost(s, host)) ?? null;
  };

  /** The store page an address is, with the path's match (ids in the path), or null. */
  ns.pageFor = (url) => {
    const store = ns.storeForUrl(url);
    if (!store) return null;
    const { pathname } = new URL(url);
    for (const page of store.pages) {
      const match = new RegExp(page.path).exec(pathname);
      if (match) return { store, page, match };
    }
    return null;
  };
})();
