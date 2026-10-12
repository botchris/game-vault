// Labels on store pages: what they say (pure models, tested in Node) and how they are drawn (a
// closed shadow root with its own styles: the store's styles do not reach in, ours do not leak out).
// The store's own scripts share the page's DOM and its layout, so nothing they can read from outside
// the closed shadow root may depend on the answer: every host carries the same empty data-gamevault
// marker and the same inline style, with a fixed width and height (the text is cut to fit and the
// full detail is a tooltip inside the shadow root), and its kind stays in the extension. Even the
// invisible "already checked" mark is a host of that same box, so a label's presence and size are
// the same whatever the user owns. Store rules cannot hide or resize the host: its declarations
// are !important, and inline !important beats any stylesheet.
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const MAX_COPIES = 3;
  const roots = new WeakMap(); // host → its closed shadow root, for the extension's own tests
  const kinds = new WeakMap(); // host → its label's kind (owned, possible, not-owned, unavailable, none)
  const sheets = new WeakMap(); // document → the labels' constructed style sheet

  /** The host's inline style: the same for every answer (see above). */
  ns.HOST_STYLE = 'all: initial !important; display: inline-block !important; box-sizing: border-box !important; '
    + 'width: 280px !important; height: 24px !important; overflow: hidden !important; '
    + 'vertical-align: middle !important; margin: 0 0 0 10px !important;';

  ns.LABEL_CSS = `
    .gv { display: flex; align-items: center; gap: 6px; width: 100%; height: 100%; padding: 0 10px; border-radius: 999px;
      font: 600 12px/24px system-ui, -apple-system, "Segoe UI", sans-serif; text-decoration: none; box-sizing: border-box; }
    .gv::before { content: ""; flex: none; width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
    .text { flex: 1 1 auto; min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
    .none { visibility: hidden; }
    .owned { background: #181c2c; color: #ffffff; }
    .possible { background: #edf0f5; color: #181c2c; outline: 1px dashed #181c2c; outline-offset: -1px; }
    .muted { background: #edf0f5; color: #4b5266; }
    a.gv:hover { filter: brightness(1.15); }
    a.gv:focus-visible { outline: 2px solid #ffb020; outline-offset: 2px; }
  `;

  function kindWord(copy, t) {
    if (copy.kind === 'physical') return t('kindDisc');
    if (copy.kind === 'key') return t(copy.pendingKey ? 'kindKeyPending' : 'kindKey');
    return t('kindLibrary');
  }

  /** "PS4 · disc, Steam · key not redeemed": each different copy once, at most three, then "+N". */
  ns.copiesText = (copies, t) => {
    const parts = [...new Set((copies ?? []).map((c) => `${c.platform || c.system} · ${kindWord(c, t)}`))];
    const shown = parts.slice(0, MAX_COPIES).join(', ');
    return parts.length > MAX_COPIES ? `${shown} ${t('moreCopies', [String(parts.length - MAX_COPIES)])}` : shown;
  };

  const HINTS = { token: 'unavailableToken', signin: 'unavailableSignIn', setup: 'unavailableSetup' };

  /** The label when Game Vault could not answer: never "not in your collection". */
  ns.unavailableModel = (reason, t) => ({
    kind: 'unavailable',
    tone: 'muted',
    text: t('labelUnavailable'),
    title: t(HINTS[reason] ?? 'unavailableUnreachable'),
  });

  /** The product label for one item's answer: {status: 'ok', result} or {status: 'unavailable', reason}. */
  ns.labelModel = (answer, t) => {
    if (answer?.status !== 'ok' || !answer.result) return ns.unavailableModel(answer?.reason, t);
    const r = answer.result;
    if (r.status === 'owned' && r.game) {
      const copies = ns.copiesText(r.game.copies, t);
      return {
        kind: 'owned',
        tone: 'owned',
        text: copies ? `${t('labelOwned')}: ${copies}` : t('labelOwned'),
        title: r.game.title,
        href: r.game.url,
      };
    }
    if (r.status === 'possible' && r.candidates?.length) {
      const titles = r.candidates.map((c) => c.title);
      return { kind: 'possible', tone: 'possible', text: t('labelPossible', [titles.join(', ')]), href: r.candidates[0].url };
    }
    return { kind: 'not-owned', tone: 'muted', text: t('labelNotOwned') };
  };

  /**
   * Gives a shadow root the labels' styles: a constructed sheet, which a store's style-src policy
   * does not govern, or a <style> where constructed sheets are missing.
   */
  function addStyles(doc, root) {
    const Sheet = doc.defaultView?.CSSStyleSheet;
    if (typeof Sheet === 'function' && Array.isArray(root.adoptedStyleSheets)) {
      try {
        let sheet = sheets.get(doc);
        if (!sheet) {
          sheet = new Sheet();
          sheet.replaceSync(ns.LABEL_CSS);
          sheets.set(doc, sheet);
        }
        root.adoptedStyleSheets = [sheet];
        return;
      } catch {
        // An engine that has the API but refuses it: fall back to a <style>.
      }
    }
    const style = doc.createElement('style');
    style.textContent = ns.LABEL_CSS;
    root.append(style);
  }

  /**
   * Draws a label at the end of parent, in a closed shadow root, and returns its host and root.
   * Text is set as text, never as HTML, and only http(s) links are kept. A model of kind 'none' is
   * an invisible mark that only says "already checked", in the same box as any label.
   */
  ns.renderLabel = (doc, parent, model) => {
    const host = doc.createElement('span');
    host.setAttribute('data-gamevault', '');
    host.setAttribute('style', ns.HOST_STYLE);
    kinds.set(host, model.kind);
    const root = host.attachShadow({ mode: 'closed' });
    roots.set(host, root);
    addStyles(doc, root);
    const visible = model.kind !== 'none';
    const href = visible && /^https?:\/\//.test(model.href ?? '') ? model.href : '';
    const el = doc.createElement(href ? 'a' : 'span');
    el.className = visible ? `gv ${model.tone}` : 'gv none';
    if (visible) {
      const text = doc.createElement('span');
      text.className = 'text';
      text.textContent = model.text;
      el.append(text);
      el.title = [model.text, model.title].filter(Boolean).join('\n');
    }
    if (href) {
      el.href = href;
      el.target = '_blank';
      el.rel = 'noopener noreferrer';
    }
    root.append(el);
    parent.append(host);
    return { host, root };
  };

  /** The shadow root of a label's host (the extension's tests read labels through it). */
  ns.rootOf = (host) => roots.get(host);

  /** The kind of label a host shows (owned, possible, not-owned, unavailable, none), or undefined. */
  ns.kindOf = (host) => kinds.get(host);
})();
