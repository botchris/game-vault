// The home page's pure helpers: what is pending, the collection in figures, and how an external
// block reads. The pending strip counts keys with the library's own rules (which keys are pending
// and which expire, from lib/deadlines.ts; the copies' `redundant` flag), while the library views
// its chips open count games. Structural types only, Node-tested (tests/home.test.mjs); the page
// is features/home/HomePage.tsx.
import { daysUntil, expiresSoon, isPendingKey } from './deadlines.ts';
import { sinceText, worstLevel } from './health.ts';

type Translate = (key: string, options?: Record<string, string>) => string;

// The API's numbers (proto/gamevault/v1/game.proto): CopyKind, CopyStatus, PlayStatus.
const KIND_PHYSICAL = 3;
const GONE = new Set([4, 5, 8]); // gifted, expired, sold: the user no longer has the copy
const PLAY_BACKLOG = 1;

/** How many covers "Recently added" shows, and how many systems the share bar names. */
const RECENT = 8;
const TOP_SYSTEMS = 5;

const DAY = 86_400_000;

/** A copy as the helpers need it (see Copy in the API). */
export interface CopyLike {
  details?: { kind: number; status: number; redeemBy: string };
  redundant: boolean;
  effectiveSystem: string;
}

/** A game as the helpers need it (see Game in the API). */
export interface GameLike {
  id: string;
  title: string;
  copies: CopyLike[];
  createdAt?: { seconds: bigint };
  playStatus: number;
}

const isHeld = (c: CopyLike) => !GONE.has(c.details?.status ?? 0);

/** One chip of the pending strip. */
export type PendingItem =
  | { kind: 'expiring'; count: number; nearest: { title: string; days: number; gameId: string }; href: string }
  | { kind: 'pending' | 'redundant'; count: number; href: string }
  | { kind: 'health'; count: number; level: 'error' | 'warning'; href: string };

/** The pending strip: keys that expire soon (the library's "Expiring" rule, past deadlines
 *  included) with the nearest one, keys still to redeem, keys not needed, and the Health issues.
 *  Empty when nothing is pending. */
export function pendingItems(games: GameLike[], issues: { level: string }[], today: Date): PendingItem[] {
  let pending = 0;
  let redundant = 0;
  let expiring = 0;
  let nearest: { title: string; days: number; gameId: string } | null = null;

  for (const g of games) {
    for (const c of g.copies) {
      if (!isPendingKey(c)) continue;
      pending++;
      if (c.redundant) redundant++;
      const days = daysUntil(c.details?.redeemBy ?? '', today);
      if (days === null || !expiresSoon(days)) continue;
      expiring++;
      if (!nearest || days < nearest.days) nearest = { title: g.title, days, gameId: g.id };
    }
  }

  const out: PendingItem[] = [];
  if (nearest) out.push({ kind: 'expiring', count: expiring, nearest, href: '#/library/expiring' });
  if (pending) out.push({ kind: 'pending', count: pending, href: '#/library/pending' });
  if (redundant) out.push({ kind: 'redundant', count: redundant, href: '#/library/redundant' });
  const level = worstLevel(issues);
  if (level) out.push({ kind: 'health', count: issues.length, level, href: '#/system/health' });
  return out;
}

/** The collection in figures. */
export interface CollectionSummary {
  games: number;
  /** Copies the user still has. */
  copies: number;
  /** Games added in the current month, in local time. */
  addedThisMonth: number;
  /** Games whose play status is "Backlog". */
  backlog: number;
  /** The newest games' ids, newest first. */
  recent: string[];
  /** Editions (a game on a system) per system, largest first; `rest` adds up the smaller ones. */
  systems: { system: string; count: number; rest: boolean }[];
}

const createdMs = (g: GameLike) => Number(g.createdAt?.seconds ?? 0n) * 1000;

/** The collection in figures, with `now` deciding the current month (local time). */
export function collectionSummary(games: GameLike[], now: Date): CollectionSummary {
  let copies = 0;
  let addedThisMonth = 0;
  let backlog = 0;
  const bySystem = new Map<string, number>();

  for (const g of games) {
    const held = g.copies.filter(isHeld);
    copies += held.length;
    for (const system of new Set(held.map((c) => c.effectiveSystem).filter(Boolean))) {
      bySystem.set(system, (bySystem.get(system) ?? 0) + 1);
    }
    const created = new Date(createdMs(g));
    if (g.createdAt && created.getFullYear() === now.getFullYear() && created.getMonth() === now.getMonth()) addedThisMonth++;
    if (g.playStatus === PLAY_BACKLOG) backlog++;
  }

  const ranked = [...bySystem].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const systems = ranked.slice(0, TOP_SYSTEMS).map(([system, count]) => ({ system, count, rest: false }));
  const rest = ranked.slice(TOP_SYSTEMS).reduce((n, [, count]) => n + count, 0);
  if (rest) systems.push({ system: '', count: rest, rest: true });

  const recent = [...games].sort((a, b) => createdMs(b) - createdMs(a)).slice(0, RECENT).map((g) => g.id);

  return { games: games.length, copies, addedThisMonth, backlog, recent, systems };
}

/** The collection's value: the first price source that priced copies (System shows them all). */
export function pickValue(totals: { copies: number; sell?: { amountMinor: bigint; currency: string } }[]):
  { amountMinor: bigint; currency: string } | null {
  const first = totals.find((x) => x.copies > 0 && x.sell);
  return first?.sell ? { amountMinor: first.sell.amountMinor, currency: first.sell.currency } : null;
}

/** What an external block shows. */
export type BlockMode = 'data' | 'notConfigured' | 'neverFetched';

/** What an external block shows for its state; a block the server left out is not configured. */
export function blockMode(b?: { state: string }): BlockMode {
  if (!b || b.state === 'not_configured') return 'notConfigured';
  return b.state === 'never_fetched' ? 'neverFetched' : 'data';
}

/** The line under a stale block: how old its data is, and why it was not refreshed when known. */
export function staleLine(b: { state: string; errorKind: string; provider: string }, fetchedAt: Date | null, now: Date,
  locale: string, t: Translate): string | null {
  if (b.state !== 'stale') return null;
  const age = fetchedAt ? t('home.updated', { when: sinceText(fetchedAt, now, locale) }) : t('home.neverUpdated');
  return b.errorKind ? `${age} · ${t(`home.errors.${b.errorKind}`, { provider: b.provider })}` : age;
}

/** Whole days until a deal ends (0: today); null when it does not say or is over. */
export function endsInDays(expires: Date | null, now: Date): number | null {
  if (!expires || expires.getTime() <= now.getTime()) return null;
  return Math.floor((expires.getTime() - now.getTime()) / DAY);
}

/** Whether the user may refresh a block now. */
export function canRefresh(refreshableAt: Date | null, now: Date): boolean {
  return !refreshableAt || refreshableAt.getTime() <= now.getTime();
}

/** A library view the home page links to ("#/library/<target>"). */
export type LibraryPreset =
  | { quick: 'pending' | 'expiring' | 'redundant' }
  | { play: number }
  | { kind: number }
  | { sort: 'added' };

/** The library view an address's target names, or null when it names none. */
export function libraryPreset(target: string): LibraryPreset | null {
  switch (target) {
    case 'pending':
    case 'expiring':
    case 'redundant':
      return { quick: target };
    case 'backlog':
      return { play: PLAY_BACKLOG };
    case 'physical':
      return { kind: KIND_PHYSICAL };
    case 'recent':
      return { sort: 'added' };
    default:
      return null;
  }
}
