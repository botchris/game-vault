// Store pages where Game Vault Connector shows what you own. Data, not code: when a store changes
// its pages, fix the patterns and selectors here and the sample page in tests/samples/. A page's
// `path` is a regular expression on the address's path; every selector list is tried in order and
// the first that matches wins. The selectors were checked on the live sites when this feature was
// built (see docs/technical.md).
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
      pages: [
        {
          kind: 'product',
          path: '^/store/[^/?#]+/?$',
          title: ['h1.human_name-view', '.product-header-view h1'],
          anchor: ['h1.human_name-view', '.product-header-view h1'],
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
      ],
    },
  ];

  /** The store a page address belongs to (by exact host name), or null. */
  ns.storeForUrl = (url) => {
    let host;
    try {
      host = new URL(url).hostname;
    } catch {
      return null;
    }
    return ns.STORES.find((s) => s.host === host) ?? null;
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
