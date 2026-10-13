// The app's addresses, "#/<page>/<target>": one parser for every page, so a new deep link (a
// field, a log line) adds a case here instead of another listener. Plain functions, Node-tested;
// the hook that follows the address is useHashRoute.ts.
import { gameIdFromHash } from './gameRoute.ts';

/** The app's pages, as their addresses name them. */
export const PAGES = ['library', 'scan', 'sources', 'providers', 'fields', 'system', 'logs', 'settings'] as const;

export type Page = (typeof PAGES)[number];

/** What an address names. */
export interface Route {
  page: Page;
  /** What to open on the page ("#/sources/<id>" → the source's id), '' for nothing. */
  target: string;
  /** The game whose sheet the library opens ("#/game/<id>"), '' for none. */
  game: string;
}

/** The route an address names; anything unknown is the library. */
export function parseHash(hash: string): Route {
  const game = gameIdFromHash(hash);
  if (game) return { page: 'library', target: '', game };
  const [first = '', rest = ''] = hash.replace(/^#\/?/, '').split(/\/(.*)/s);
  const page = (PAGES as readonly string[]).includes(first) ? first as Page : 'library';
  if (page !== first) return { page, target: '', game: '' };
  let target = '';
  try {
    target = decodeURIComponent(rest);
  } catch {
    // A broken escape (a mangled link) opens nothing rather than breaking the page.
  }
  return { page, target, game: '' };
}
