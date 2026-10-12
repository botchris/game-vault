// Shared by the extension's store-page tests: a DOM for saved pages (happy-dom, a dev dependency of
// the web, loaded from web/node_modules; nothing is added to the extension), chrome.i18n's message
// lookup over _locales, and a content-script controller following a page.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import '../ownership/stores.js';
import '../ownership/read.js';
import '../ownership/label.js';
import '../ownership/content.js';

/** The content scripts' namespace. */
export const ns = globalThis.gvOwnership;

const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { Window } = require('happy-dom');

/** A window showing html at url; scripts never run. Close it with window.happyDOM.close(). */
export function pageWindow(html, url) {
  const window = new Window({
    url,
    settings: { disableJavaScriptEvaluation: true, disableJavaScriptFileLoading: true, disableCSSFileLoading: true, disableIframePageLoading: true },
  });
  window.document.write(html);
  return window;
}

/** A saved store page from tests/samples/ (invented titles). */
export function sample(file) {
  return readFileSync(new URL(`./samples/${file}`, import.meta.url), 'utf8');
}

/** chrome.i18n.getMessage over a locale's messages.json: named placeholders, then $1… substitutions. */
export function makeT(lang = 'en') {
  const messages = JSON.parse(readFileSync(new URL(`../_locales/${lang}/messages.json`, import.meta.url), 'utf8'));
  return (key, subs = []) => {
    const m = messages[key];
    if (!m) return '';
    const list = Array.isArray(subs) ? subs : [subs];
    return m.message
      .replace(/\$([A-Za-z0-9_@]+)\$/g, (_, name) => m.placeholders?.[name.toLowerCase()]?.content ?? '')
      .replace(/\$(\d)/g, (_, n) => String(list[Number(n) - 1] ?? ''));
  };
}

/** Waits for debounced scans and MutationObserver callbacks. */
export const settle = (ms = 40) => new Promise((resolve) => setTimeout(resolve, ms));

/** The text a label shows (its shadow root is closed to the page, not to the extension). */
export const labelText = (host) => ns.rootOf(host)?.querySelector('.gv')?.textContent ?? '';

/**
 * How many labels of a kind (owned, possible, not-owned, unavailable, none) the document holds. The
 * kind is not in the page's DOM, so the store's scripts cannot read what the user owns.
 */
export const countKind = (doc, kind) => [...doc.querySelectorAll('[data-gamevault]')].filter((host) => ns.kindOf(host) === kind).length;

/** A lookup result for a game the user owns, as a Steam library copy. */
export const owned = (title) => ({
  status: 'owned',
  game: { title, url: 'http://127.0.0.1:8093/#/game/g1', copies: [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }] },
  candidates: [],
});

/** A lookup result for a game the user does not own. */
export const notOwned = () => ({ status: 'not_owned', game: null, candidates: [] });

/**
 * Follows window's page as the content script does; url.current is the address, send answers
 * lookups. Returns the lookups sent and the controller.
 */
export function follow(window, url, send, t = makeT('en')) {
  const sent = [];
  const ctl = ns.start({
    doc: window.document,
    url: () => url.current,
    send: async (items) => {
      sent.push(items);
      return send(items);
    },
    t,
    MutationObserver: window.MutationObserver,
    setTimer: (fn, ms) => setTimeout(fn, ms),
    clearTimer: (h) => clearTimeout(h),
    debounceMs: 5,
  });
  return { sent, ctl };
}

/** Stops the controller and closes the window when the test ends. */
export const done = (tt, window, ctl) => tt.after(async () => {
  ctl.stop();
  await window.happyDOM.close();
});
