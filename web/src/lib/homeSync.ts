// How the home page keeps its two external blocks current while reads and refreshes overlap. Every
// refresh, once it settles (answered, refused or failed), reads the whole home again, and only the
// newest read may replace what the page shows. So a refresh of one block never drops or overwrites
// the other block's newer answer: whichever refresh settles last is followed by a read that sees
// both. The refresh's own answer is not shown, since a read started before it could still arrive
// after it. Structural types only, Node-tested (tests/homeSync.test.mjs).
import { latestOnly } from './latest.ts';

/** What a refresh came to: whether it failed and why, and the home read after it (null when that
 *  read failed or a newer one took over). */
export interface RefreshOutcome<T> {
  failed: boolean;
  error: unknown;
  fresh: T | null;
}

/** The home page's reads and refreshes. */
export interface HomeSync<T> {
  /** Reads the home; resolves to it, or null when the read failed or a newer one took over. */
  load(): Promise<T | null>;
  /** Sends a refresh, then reads the home again whatever its answer. Never rejects. */
  refresh(send: () => Promise<unknown>): Promise<RefreshOutcome<T>>;
}

/** Builds the page's sync: `read` gets the home, `onRead` shows a read that is still the newest,
 *  `onError` reports a failed read that is still the newest. */
export function homeSync<T>(read: () => Promise<T>, onRead: (home: T) => void, onError: (e: unknown) => void): HomeSync<T> {
  const latest = latestOnly();

  const load = async (): Promise<T | null> => {
    const current = latest();
    try {
      const home = await read();
      if (!current()) return null;
      onRead(home);
      return home;
    } catch (e) {
      if (current()) onError(e);
      return null;
    }
  };

  const refresh = async (send: () => Promise<unknown>): Promise<RefreshOutcome<T>> => {
    let failed = false;
    let error: unknown = null;
    try {
      await send();
    } catch (e) {
      failed = true;
      error = e;
    }
    return { failed, error, fresh: await load() };
  };

  return { load, refresh };
}
