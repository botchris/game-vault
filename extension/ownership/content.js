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
        const fresh = read.items.filter((e) => {
          const host = ns.labelOf(e.element);
          if (!host) return true;
          const key = asked.get(host);
          if (key === undefined || key === keyOf(e.item)) return false;
          host.remove(); // the element shows another item now: its label is stale
          return true;
        });
        if (!fresh.length) return;
        const outcome = await ask(fresh.map((e) => e.item));
        if (url() !== href) {
          // The page changed while Game Vault answered: drop the answer and read the new page.
          again = true;
          return;
        }
        if (stopped) return; // the page was left while Game Vault answered: draw nothing
        fresh.forEach((e, i) => {
          if (ns.hasLabel(e.element)) return;
          const { host } = ns.renderLabel(doc, e.element, ns.labelModel(answerAt(outcome, i), t));
          asked.set(host, keyOf(e.item));
        });
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
