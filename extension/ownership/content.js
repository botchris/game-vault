// Content script on the store sites the user allowed: reads the page (read.js), asks the background
// what the user owns, and draws the labels (label.js). Single-page navigation and late-loaded
// content are followed with a debounced MutationObserver; only new items are asked about.
(() => {
  const ns = (globalThis.gvOwnership ??= {});
  const UNREACHABLE = { status: 'unavailable', reason: 'unreachable' };

  const answerAt = (outcome, i) => (outcome?.status === 'ok' ? { status: 'ok', result: outcome.results?.[i] } : outcome);

  /**
   * Starts following a page. deps: doc; url() (the current address); send(items), resolving to
   * {status: 'ok', results} or {status: 'unavailable', reason}; t (chrome.i18n.getMessage);
   * MutationObserver; setTimer(fn, ms) and clearTimer(handle); debounceMs. Returns {scan, stop}.
   */
  ns.start = (deps) => {
    const { doc, url, t, debounceMs = 400 } = deps;
    let timer = null;
    let running = false;
    let again = false;
    let lastUrl = '';

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
        const fresh = read.items.filter((e) => !ns.hasLabel(e.element));
        if (!fresh.length) return;
        const outcome = await ask(fresh.map((e) => e.item));
        if (url() !== href) return; // the page changed while Game Vault answered
        fresh.forEach((e, i) => {
          if (!ns.hasLabel(e.element)) ns.renderLabel(doc, e.element, ns.labelModel(answerAt(outcome, i), t));
        });
      } finally {
        running = false;
        if (again) {
          again = false;
          schedule();
        }
      }
    }

    function schedule() {
      if (timer !== null) deps.clearTimer(timer);
      timer = deps.setTimer(() => {
        timer = null;
        scan();
      }, debounceMs);
    }

    const observer = new deps.MutationObserver(schedule);
    observer.observe(doc.documentElement, { childList: true, subtree: true, characterData: true });
    schedule();
    return {
      scan,
      stop() {
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
