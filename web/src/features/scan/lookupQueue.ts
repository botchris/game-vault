// Looks scanned codes up one at a time, in order: the barcode databases behind IdentifyBarcode
// have rate limits, and a shelf scanned in a minute must not become thirty requests at once.

export interface LookupQueue {
  /** Queues a row's code; a row already waiting is not queued twice. */
  push(id: string, code: string): void;
  /** Forgets a waiting row (it was removed from the list). */
  drop(id: string): void;
}

export function createLookupQueue<A>(
  lookup: (code: string) => Promise<A>,
  done: (id: string, answer?: A, error?: unknown) => void,
): LookupQueue {
  const waiting: { id: string; code: string }[] = [];
  let current = '';
  let running = false;

  const pump = async () => {
    if (running) return;
    running = true;
    while (waiting.length) {
      const next = waiting.shift()!;
      current = next.id;
      try {
        done(next.id, await lookup(next.code));
      } catch (e) {
        done(next.id, undefined, e);
      }
    }
    current = '';
    running = false;
  };

  return {
    push(id, code) {
      if (id === current || waiting.some((w) => w.id === id)) return;
      waiting.push({ id, code });
      void pump();
    },
    drop(id) {
      const i = waiting.findIndex((w) => w.id === id);
      if (i >= 0) waiting.splice(i, 1);
    },
  };
}
