// Editions in the web: a game on each system it is played on, as the library lists them ("By
// platform": one item per edition; "By game": one per game), the library filters, the System
// options, an edition's copies and the order in which a card tries covers. Structural types only,
// so Node tests run it as is (no generated code, no TypeScript enums); imports carry the .ts
// extension.
import { matchesFieldFilters, pruneFieldFilter, type Definition, type FieldFilter, type Values } from './fields.ts';

export interface EditionLike { system: string; coverUrl: string; coverPhotoId: string; main: boolean }
export interface CopyLike {
  effectiveSystem: string;
  sourceId: string;
  details?: { kind: number; platform: string; fields: Values };
}
export interface GameLike {
  id: string;
  title: string;
  genres: string[];
  playStatus: number;
  fields: Values;
  copies: CopyLike[];
  editions: EditionLike[];
}

export type Grouping = 'platform' | 'game';

/** One card or row of the library: a game's edition ("By platform") or the game itself ("By game"). */
export interface LibraryItem<G extends GameLike> {
  key: string;
  game: G;
  /** The edition's system; in "By game", the main edition's ('' for a game without copies). */
  system: string;
  /** The game as filters and sorts see it: in "By platform", with only the edition's copies. */
  view: G;
}

/** Systems offered in the copy form: PC, then the console names game.SystemOf knows. */
export const SYSTEMS = [
  'PC', 'PS5', 'PS4', 'PS3', 'PS2', 'PS1', 'PSP', 'PS Vita', 'Xbox Series', 'Xbox One', 'Xbox 360', 'Xbox',
  'Switch', 'Wii U', 'Wii', 'GameCube', 'N64', '3DS', 'DS', 'Game Boy',
];
const PC_PLATFORMS = [
  'steam', 'epic games', 'gog', 'ea app', 'ubisoft connect', 'battle.net', 'itch.io', 'amazon games', 'rockstar', 'riot',
  'battlestate (tarkov)', 'pc',
];
const STORE_SYSTEMS: Record<string, string> = { 'playstation store': 'PS4', 'nintendo eshop': 'Switch', 'microsoft store / xbox': 'PC' };

/** The system a platform implies; mirrors game.SystemOf in the server. */
export function systemOf(platform: string): string {
  const p = platform.trim();
  if (!p) return 'Other';
  const k = p.toLowerCase();
  if (PC_PLATFORMS.includes(k)) return 'PC';
  if (STORE_SYSTEMS[k]) return STORE_SYSTEMS[k]!;
  return SYSTEMS.find((s) => s.toLowerCase() === k) ?? p;
}

/** The system of the game's main edition; '' for a game without copies. */
export function mainSystem(g: { editions: EditionLike[] }): string {
  return g.editions.find((e) => e.main)?.system ?? '';
}

/** The edition a sheet opened on `system` shows: that one while the game has it, else the main one
 *  (no system asked for, or the edition's copies moved away). */
export function openEdition(g: { editions: EditionLike[] }, system: string): string {
  return g.editions.some((e) => e.system === system) ? system : mainSystem(g);
}

/** Path of an edition's cover (URL-escaped: systems may hold spaces or slashes); without a system,
 *  the main edition's. */
export function coverPath(gameId: string, system: string): string {
  const base = `/media/covers/${encodeURIComponent(gameId)}`;
  return system ? `${base}/${encodeURIComponent(system)}` : base;
}

/** A game's editions as library items, in the server's order (main first); a game without copies
 *  is one item with no system. */
export function editionsOf<G extends GameLike>(g: G): LibraryItem<G>[] {
  if (g.editions.length === 0) return [{ key: g.id, game: g, system: '', view: g }];
  return g.editions.map((e) => ({
    key: `${g.id}|${e.system}`,
    game: g,
    system: e.system,
    view: { ...g, copies: g.copies.filter((c) => c.effectiveSystem === e.system) } as G,
  }));
}

/** The library's items: every edition ("By platform") or every game on its main edition ("By game"). */
export function libraryItems<G extends GameLike>(games: G[], grouping: Grouping): LibraryItem<G>[] {
  return grouping === 'game'
    ? games.map((g) => ({ key: g.id, game: g, system: mainSystem(g), view: g }))
    : games.flatMap((g) => editionsOf(g));
}

/** The accessible name of an item's open button: in "By platform" two editions of a game share a
 *  title, so the name adds the edition's system. Undefined keeps the button's own text. */
export function itemLabel(
  t: (key: string, values: { title: string; system: string }) => string,
  item: LibraryItem<GameLike>,
  grouping: Grouping,
): string | undefined {
  if (grouping !== 'platform' || !item.system) return undefined;
  return t('library.itemLabel', { title: item.game.title, system: item.system });
}

export interface Filters {
  /** A copy kind (CopyKind's number); 0: any. */
  kind: number;
  platforms: string[];
  /** Systems (PC, PS4…): an item matches when one of its copies is on one of them. */
  systems: string[];
  genres: string[];
  /** Source ids; MANUAL stands for copies added by hand or from a CSV. */
  sources: string[];
  /** Play statuses (PlayStatus's numbers); 0 stands for games the user has not given one. */
  play: number[];
  /** Custom field values by field id: choice ids, 'yes' / 'no', or '' for no value. */
  fields: FieldFilter;
}

export const MANUAL = '';

export const NO_FILTERS: Filters = { kind: 0, platforms: [], systems: [], genres: [], sources: [], play: [], fields: {} };

export const activeFilterCount = (f: Filters) => (f.kind ? 1 : 0) + f.platforms.length + f.systems.length + f.genres.length
  + f.sources.length + f.play.length + Object.values(f.fields).reduce((n, keys) => n + keys.length, 0);

/** The filters without custom field values that no longer exist (a deleted field, a removed
 *  choice). The same object when nothing was dropped. */
export function pruneFilters(f: Filters, defs: Definition[]): Filters {
  const fields = pruneFieldFilter(f.fields, defs);
  return fields === f.fields ? f : { ...f, fields };
}

/** A game (or an edition's view of it) matches when it has a copy of the kind on one of the
 *  platforms, a copy on one of the systems, a copy from one of the sources, one of the genres, one of
 *  the play statuses and one of the chosen values of each filtered custom field. */
export function matchesFilters(g: GameLike, f: Filters, defs: Definition[]): boolean {
  if (f.kind || f.platforms.length) {
    const ok = g.copies.some((c) => (!f.kind || c.details?.kind === f.kind) && (!f.platforms.length || f.platforms.includes(c.details?.platform ?? '')));
    if (!ok) return false;
  }
  if (f.systems.length && !g.copies.some((c) => f.systems.includes(c.effectiveSystem))) return false;
  if (f.sources.length && !g.copies.some((c) => f.sources.includes(c.sourceId))) return false;
  if (f.genres.length && !g.genres.some((x) => f.genres.includes(x))) return false;
  if (f.play.length && !f.play.includes(g.playStatus)) return false;
  return matchesFieldFilters(g, defs, f.fields);
}

/** The items that pass the filters. Copy-level checks (kind, platform, system, source, copy fields
 *  and `copyLevel`, the quick filters) look at the item's view, so in "By platform" an edition stays
 *  when one of its copies matches; game-level ones (genres, play status, game fields and `gameLevel`,
 *  the search) see the same game in every edition. */
export function filterItems<G extends GameLike>(items: LibraryItem<G>[], f: Filters, defs: Definition[],
  checks: { copyLevel?: (view: G) => boolean; gameLevel?: (game: G) => boolean } = {}): LibraryItem<G>[] {
  return items.filter((i) => matchesFilters(i.view, f, defs)
    && (!checks.copyLevel || checks.copyLevel(i.view))
    && (!checks.gameLevel || checks.gameLevel(i.game)));
}

/** How many items are on each system, most first then by name: pass the items' views, so editions
 *  are counted in "By platform" and games in "By game". */
export function systemCounts(views: GameLike[]): [string, number][] {
  const counts = new Map<string, number>();
  for (const v of views) for (const s of new Set(v.copies.map((c) => c.effectiveSystem))) counts.set(s, (counts.get(s) ?? 0) + 1);
  return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

/** The items sorted by `cmp` on their views (so "Most copies" counts an edition's copies), ties by system. */
export function sortItems<G extends GameLike>(items: LibraryItem<G>[], cmp: (a: G, b: G) => number): LibraryItem<G>[] {
  return [...items].sort((a, b) => cmp(a.view, b.view) || a.system.localeCompare(b.system));
}

/** How many items and how many distinct games a list holds ("320 editions of 300 games"). */
export function itemCounts(items: LibraryItem<GameLike>[]): { items: number; games: number } {
  return { items: items.length, games: new Set(items.map((i) => i.game.id)).size };
}

/** The systems whose cover a card tries, in order: its own edition's (the main one's when it names
 *  none), then the main edition's, then the others'. A game without copies: [''] (its only address). */
export function coverOrder(g: { editions: EditionLike[] }, system: string): string[] {
  const main = mainSystem(g);
  const all = [system || main, main, ...g.editions.map((e) => e.system)];
  return all.filter((s, i) => all.indexOf(s) === i);
}

/** The next cover to try, or null when all failed (the card then shows initials and stops asking). */
export function nextCover(order: string[], failed: ReadonlySet<string>): string | null {
  return order.find((s) => !failed.has(s)) ?? null;
}

/** A game's copies grouped by system: the current edition first, then in the editions' order. */
export function groupCopies<C extends CopyLike>(copies: C[], editions: EditionLike[], current: string): { system: string; copies: C[] }[] {
  const systems = [current, ...editions.map((e) => e.system), ...copies.map((c) => c.effectiveSystem)];
  return systems
    .filter((s, i) => s && systems.indexOf(s) === i)
    .map((system) => ({ system, copies: copies.filter((c) => c.effectiveSystem === system) }))
    .filter((g) => g.copies.length > 0);
}
