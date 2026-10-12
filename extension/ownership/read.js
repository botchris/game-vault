// Reads a store page the way its store's data describes it (stores.js): the items to look up and
// the elements their labels go in. Runs in the page (content script) and in Node tests.
(() => {
  const ns = (globalThis.gvOwnership ??= {});

  /** The first element matching one of the selectors, in the list's order. */
  ns.first = (root, selectors) => {
    for (const s of selectors ?? []) {
      const el = root?.querySelector(s);
      if (el) return el;
    }
    return null;
  };

  /** An element's text on one line. Labels live in shadow roots, so they never add to it. */
  ns.textOf = (el) => (el?.textContent ?? '').replace(/\s+/g, ' ').trim();

  /** The label host (or invisible "checked" mark) the extension put in this element, or null. */
  ns.labelOf = (el) => (el ? [...el.children].find((c) => c.hasAttribute('data-gamevault')) ?? null : null);

  /** Whether the extension already put a label (or an invisible "checked" mark) in this element. */
  ns.hasLabel = (el) => ns.labelOf(el) !== null;

  function productId(doc, found) {
    const { page, match } = found;
    if (page.id?.fromPath) return match[page.id.fromPath] ?? '';
    if (page.id?.attr) {
      const v = ns.first(doc, page.id.selectors)?.getAttribute(page.id.attr)?.trim() ?? '';
      return /^\d{1,20}$/.test(v) ? v : '';
    }
    return '';
  }

  /**
   * What the page shows, or null while it has not drawn it (or it is no game): `items` pairs each
   * item to look up ({store, id} and/or {title}) with the element its label goes in.
   */
  ns.readPage = (doc, found) => {
    const { store, page } = found;
    if (page.kind === 'product') {
      const anchor = ns.first(doc, page.anchor);
      if (!anchor) return null;
      const item = {};
      const id = productId(doc, found);
      if (id) {
        item.store = store.store;
        item.id = id;
      }
      const title = ns.textOf(ns.first(doc, page.title));
      if (title) item.title = title;
      if (!item.id && !item.title) return null;
      return { kind: 'product', items: [{ item, element: anchor }] };
    }
    return null; // a kind of page this version does not read
  };
})();
