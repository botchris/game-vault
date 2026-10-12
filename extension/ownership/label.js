// Labels on store pages: what they say (pure models, tested in Node) and how they are drawn (a
// closed shadow root with its own styles: the store's styles do not reach in, ours do not leak out).
// The store's own scripts share the page's DOM, so nothing they can read says what the user owns:
// a label's host carries an empty data-gamevault marker and its kind stays in the extension.
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const MAX_COPIES = 3;
  const roots = new WeakMap(); // host → its closed shadow root, for the extension's own tests
  const kinds = new WeakMap(); // host → its label's kind (owned, possible, not-owned, unavailable, none)

  ns.LABEL_CSS = `
    .gv { display: inline-flex; align-items: center; gap: 6px; max-width: 36em; padding: 3px 10px; border-radius: 999px;
      font: 600 12px/1.4 system-ui, -apple-system, "Segoe UI", sans-serif; text-decoration: none; white-space: nowrap;
      overflow: hidden; text-overflow: ellipsis; box-sizing: border-box; }
    .gv::before { content: ""; flex: none; width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
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
      return { kind: 'possible', tone: 'possible', text: t('labelPossible', [titles.join(', ')]), title: titles.join('\n'), href: r.candidates[0].url };
    }
    return { kind: 'not-owned', tone: 'muted', text: t('labelNotOwned') };
  };

  /**
   * Draws a label at the end of parent, in a closed shadow root, and returns its host and root.
   * Text is set as text, never as HTML, and only http(s) links are kept. A model of kind 'none' is
   * an invisible mark that only says "already checked".
   */
  ns.renderLabel = (doc, parent, model) => {
    const host = doc.createElement('span');
    host.setAttribute('data-gamevault', '');
    host.setAttribute('style', model.kind === 'none'
      ? 'display: none !important;'
      : `all: initial; display: ${model.block ? 'block' : 'inline-block'}; vertical-align: middle; margin: ${model.block ? '6px 0 0' : '0 0 0 10px'};`);
    kinds.set(host, model.kind);
    const root = host.attachShadow({ mode: 'closed' });
    roots.set(host, root);
    if (model.kind !== 'none') {
      const style = doc.createElement('style');
      style.textContent = ns.LABEL_CSS;
      const href = /^https?:\/\//.test(model.href ?? '') ? model.href : '';
      const el = doc.createElement(href ? 'a' : 'span');
      el.className = `gv ${model.tone}`;
      el.textContent = model.text;
      if (model.title) el.title = model.title;
      if (href) {
        el.href = href;
        el.target = '_blank';
        el.rel = 'noopener noreferrer';
      }
      root.append(style, el);
    }
    parent.append(host);
    return { host, root };
  };

  /** The shadow root of a label's host (the extension's tests read labels through it). */
  ns.rootOf = (host) => roots.get(host);

  /** The kind of label a host shows (owned, possible, not-owned, unavailable, none), or undefined. */
  ns.kindOf = (host) => kinds.get(host);
})();
