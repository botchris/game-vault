// The recipe engine: opens the store's sign-in, waits until the user is signed in, captures the
// value and closes the tab. The browser is behind `api` (see background.js), so Node tests drive
// it with a fake one.

import { validate } from './recipe.js';
import { cookieHeader, jsonField, prefixMatch, redirectValue } from './capture.js';

// How often the engine checks for a capture between page loads.
const POLL_MS = 3000;

function failure(code, message) {
  const e = new Error(message ?? code);
  e.code = code;
  return e;
}

/** Starts a recipe; result resolves with the captured value or rejects with a coded error. */
export function startRun(recipe, api) {
  const { kind, timeoutMs } = validate(recipe); // throws before anything opens
  const capture = recipe.capture[kind];
  let tabId;
  let done = false;
  let unsubscribe = () => {};
  let stopTimer = () => {};
  let stopPoll = () => {};
  let resolve;
  let reject;
  const result = new Promise((res, rej) => { resolve = res; reject = rej; });
  // Whoever runs the recipe may look at the result later (after a cancel, for example): an early
  // failure must not count as an unhandled rejection meanwhile.
  result.catch(() => {});

  const finish = (error, value, tabGone = false) => {
    if (done) return;
    done = true;
    unsubscribe();
    stopTimer();
    stopPoll();
    if (tabId !== undefined && !tabGone) api.closeTab(tabId);
    if (error) reject(error);
    else resolve(value);
  };

  const ready = async (url) => {
    const w = recipe.when;
    if (!w) return true;
    if (w.urlPrefix && !prefixMatch(url, w.urlPrefix)) return false;
    if (w.fetch && !jsonField(await api.fetchText(w.fetch.url), w.fetch.field)) return false;
    return true;
  };

  const attempt = async (url) => {
    if (done) return;
    if (kind === 'redirect') {
      const value = redirectValue(url, capture.prefix, capture.param);
      if (value) finish(null, value);
      return;
    }
    try {
      if (!(await ready(url))) return;
      let value = '';
      if (kind === 'cookie') value = await api.getCookie(capture.url, capture.name, tabId);
      if (kind === 'cookies') value = cookieHeader(await api.getCookies(capture.url, tabId));
      if (kind === 'storage' && prefixMatch(url, capture.origin + (capture.path ?? ''))) value = await api.readStorage(tabId, capture.key, capture.origin);
      if (kind === 'fetch') value = jsonField(await api.fetchText(capture.url), capture.field);
      if (value && (!recipe.when?.contains || value.includes(recipe.when.contains))) finish(null, value);
    } catch {
      // Not readable yet (a page still loading, a fetch refused): the next page load tries again.
    }
  };

  (async () => {
    try {
      tabId = await api.openTab(recipe.open, Boolean(recipe.private));
      if (done) { // cancelled while the tab was opening
        api.closeTab(tabId);
        return;
      }
      unsubscribe = api.watchTab(tabId, (event) => {
        if (event.kind === 'removed') finish(failure('cancelled', 'the sign-in tab was closed'), undefined, true);
        else if (event.kind === 'committed' && kind === 'redirect') {
          const value = redirectValue(event.url, capture.prefix, capture.param);
          if (value) finish(null, value);
        } else if (event.kind === 'loaded') attempt(event.url);
      });
      stopTimer = api.setTimer(timeoutMs, () => finish(failure('timeout', 'the sign-in took too long')));
      // Logins that finish without loading a page (a modal, a single-page app) are caught by
      // checking every few seconds too.
      stopPoll = api.setInterval(POLL_MS, async () => { if (!done) attempt(await api.tabURL(tabId)); });
      await attempt(await api.tabURL(tabId)); // a user already signed in (or redirected already) is captured at once
    } catch (e) {
      finish(failure('failed', e?.message));
    }
  })();

  return { result, cancel: () => finish(failure('cancelled', 'cancelled')) };
}
