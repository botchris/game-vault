// Content script on the store sites the user allowed: reads the page (read.js), asks the background
// what the user owns, and draws the labels (label.js). Single-page navigation and late-loaded
// content are followed with a debounced MutationObserver (with a longest wait, so a page that never
// stops changing is still read); only new items are asked about, and a label whose element now
// shows another item (a store that rewrites the title in place) is replaced.
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const UNREACHABLE = { status: 'unavailable', reason: 'unreachable' };

  /** What identifies an item: a label answers for this, and only this. */
  const keyOf = (item) => JSON.stringify([item.store ?? '', item.id ?? '', item.title ?? '']);

  const answerAt = (outcome, i) => (outcome?.status === 'ok' ? { status: 'ok', result: outcome.results?.[i] } : outcome);

  /**
   * Starts following a page. deps: doc; url() (the current address); send(items), resolving to
   * {status: 'ok', results} or {status: 'unavailable', reason}; t (chrome.i18n.getMessage);
   * MutationObserver; setTimer(fn, ms) and clearTimer(handle); debounceMs; maxWaitMs (the longest
   * a scan waits while the page keeps changing); now() (a clock in ms). Returns {scan, stop}.
   */
  ns.start = (deps) => {
    const { doc, url, t, debounceMs = 400, maxWaitMs = 2000, now = Date.now } = deps;
    const asked = new WeakMap(); // label host → keyOf the item it answers for
    let timer = null;
    let waitingSince = 0;
    let running = false;
    let again = false;
    let lastUrl = '';
    let stopped = false;

    const ask = async (items) => {
      try {
        return (await deps.send(items)) ?? UNREACHABLE;
      } catch {
        return UNREACHABLE;
      }
    };

    /** The entries without a label for their item yet. A label whose element now shows another
     *  item (a store that rewrites the title in place) is stale: it is removed first. */
    function freshItems(entries) {
      for (const e of entries) {
        const host = ns.labelOf(e.element);
        const key = host ? asked.get(host) : undefined;
        if (key !== undefined && key !== keyOf(e.item)) host.remove();
      }
      return entries.filter((e) => !ns.labelOf(e.element));
    }

    /** Whether an answer for page `href` may still be drawn. The page changed while Game Vault
     *  answered: the answer is dropped and the new page read. The page was left: nothing is drawn. */
    function answerStillWanted(href) {
      if (url() !== href) {
        again = true;
        return false;
      }
      return !stopped;
    }

    /** Draws one label per entry still without one, from the answer for the same position. */
    function drawLabels(entries, outcome, model) {
      entries.forEach((e, i) => {
        if (ns.hasLabel(e.element)) return;
        const { host } = ns.renderLabel(doc, e.element, model(answerAt(outcome, i), t));
        asked.set(host, keyOf(e.item));
      });
    }

    // A bundle's summary counts every game on it, so all of them are asked about whenever new ones
    // appear (the background's cache answers the ones already asked). Every game gets a host
    // whatever the answer, so the page cannot tell which ones are owned.
    async function labelBundle(read, href) {
      const fresh = freshItems(read.items);
      if (!fresh.length && ns.hasLabel(read.summary)) return;
      const items = read.items.map((e) => e.item);
      const outcome = await ask(items);
      if (!answerStillWanted(href)) return;
      drawLabels(read.items, outcome, ns.markModel);
      for (const old of [...read.summary.children].filter((c) => ns.kindOf(c) === 'summary')) old.remove();
      ns.renderLabel(doc, read.summary, ns.summaryModel(outcome, items, t));
    }

    async function scan() {
      if (running) {
        again = true;
        return;
      }
      running = true;
      try {
        const href = url();
        if (href !== lastUrl) {
          // Another page of a single-page site: the last page's labels no longer apply.
          lastUrl = href;
          for (const old of doc.querySelectorAll('[data-gamevault]')) old.remove();
        }
        const found = ns.pageFor(href);
        const read = found && ns.readPage(doc, found);
        if (!read) return;
        if (read.kind === 'bundle') {
          await labelBundle(read, href);
          return;
        }
        const fresh = freshItems(read.items);
        if (!fresh.length) return;
        const outcome = await ask(fresh.map((e) => e.item));
        if (!answerStillWanted(href)) return;
        drawLabels(fresh, outcome, ns.labelModel);
      } finally {
        running = false;
        if (again) {
          again = false;
          schedule();
        }
      }
    }

    /** Debounces scans, but a scan never waits more than maxWaitMs after the first change. */
    function schedule() {
      if (stopped) return; // a scan that was waiting for Game Vault when the page was left
      const at = now();
      if (timer === null) waitingSince = at;
      else deps.clearTimer(timer);
      const delay = Math.max(0, Math.min(debounceMs, waitingSince + maxWaitMs - at));
      timer = deps.setTimer(() => {
        timer = null;
        scan();
      }, delay);
    }

    const observer = new deps.MutationObserver(schedule);
    observer.observe(doc.documentElement, { childList: true, subtree: true, characterData: true });
    schedule();
    return {
      scan,
      stop() {
        stopped = true;
        observer.disconnect();
        if (timer !== null) deps.clearTimer(timer);
      },
    };
  };

  // In a store page (not in Node tests): start once per page.
  if (typeof document !== 'undefined' && globalThis.chrome?.runtime?.id && !globalThis.__gvOwnershipStarted) {
    globalThis.__gvOwnershipStarted = true;
    ns.start({
      doc: document,
      url: () => location.href,
      send: (items) => chrome.runtime.sendMessage({ op: 'ownership', items }),
      t: (key, subs) => chrome.i18n.getMessage(key, subs),
      MutationObserver,
      setTimer: (fn, ms) => setTimeout(fn, ms),
      clearTimer: (h) => clearTimeout(h),
    });
  }
})();
