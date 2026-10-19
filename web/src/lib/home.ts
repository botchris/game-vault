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

/** How many covers "Recently added" and "In your backlog" show, and how many systems get a tile. */
export const ROW = 12;
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
  | { kind: 'expired' | 'pending' | 'redundant'; count: number; href: string }
  | { kind: 'health'; count: number; level: 'error' | 'warning'; href: string };

/** The pending strip: keys whose deadline has passed, keys that expire within the library's
 *  "Expiring" window with the nearest one, keys still to redeem, keys not needed, and the Health
 *  issues. Both deadline chips open the library's "Expiring" view, which lists the two together.
 *  Empty when nothing is pending. */
export function pendingItems(games: GameLike[], issues: { level: string }[], today: Date): PendingItem[] {
  let pending = 0;
  let redundant = 0;
  let expired = 0;
  let expiring = 0;
  let nearest: { title: string; days: number; gameId: string } | null = null;

  for (const g of games) {
    for (const c of g.copies) {
      if (!isPendingKey(c)) continue;
      pending++;
      if (c.redundant) redundant++;
      const days = daysUntil(c.details?.redeemBy ?? '', today);
      if (days === null || !expiresSoon(days)) continue;
      if (days < 0) {
        expired++;
        continue;
      }
      expiring++;
      if (!nearest || days < nearest.days) nearest = { title: g.title, days, gameId: g.id };
    }
  }

  const out: PendingItem[] = [];
  if (expired) out.push({ kind: 'expired', count: expired, href: '#/library/expiring' });
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
  /** Every backlog game's id, newest first (the row shows the first twelve, the hero picks one). */
  backlogGames: string[];
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

  const newest = [...games].sort((a, b) => createdMs(b) - createdMs(a));
  const recent = newest.slice(0, ROW).map((g) => g.id);
  const backlogGames = newest.filter((g) => g.playStatus === PLAY_BACKLOG).map((g) => g.id);

  return { games: games.length, copies, addedThisMonth, backlog, recent, backlogGames, systems };
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

/** A slide of the hero: a deal (featured, or at its historical low), a release (from the user's
 *  series, or among the most anticipated), or a game of the collection (the newest, or one of the
 *  backlog). */
export type HeroSlide<D, R> =
  | { kind: 'deal' | 'low'; deal: D }
  | { kind: 'series' | 'anticipated'; release: R }
  | { kind: 'recent' | 'backlog'; gameId: string };

/** What the hero picks its slides from: the page's lists, and the collection's game ids newest
 *  first. */
export interface HeroSource<D, R> {
  featured: D[];
  lows: D[];
  series: R[];
  anticipated: R[];
  recent: string[];
  backlog: string[];
}

/** The hero's slides: one of each kind, the two deals first (the featured one, then a historical
 *  low), so the hero opens on what is for sale, then releases and games in turn. A kind with nothing to show is left out: deals and releases need art and an https link, the low
 *  is another game than the featured deal, and the backlog's game (one a day, the same all day) is
 *  never the newest game, which has a slide of its own. */
export function heroSlides<D extends { title: string; imageUrl: string; url: string }, R extends { title: string; coverUrl: string; url: string }>(
  src: HeroSource<D, R>, today: Date): HeroSlide<D, R>[] {
  const showable = <T extends { url: string }>(art: (x: T) => string) => (x: T) => !!art(x) && !!httpsLink(x.url);
  const deals = src.featured.filter(showable((d: D) => d.imageUrl));
  const deal = deals[0];
  const low = src.lows.filter(showable((d: D) => d.imageUrl)).find((d) => d.title !== deal?.title);
  const series = src.series.find(showable((r: R) => r.coverUrl));
  const anticipated = src.anticipated.filter(showable((r: R) => r.coverUrl)).find((r) => r.title !== series?.title);
  const newest = src.recent[0];
  const backlog = src.backlog.filter((id) => id !== newest);
  const day = Math.floor(Date.UTC(today.getFullYear(), today.getMonth(), today.getDate()) / DAY);

  const out: HeroSlide<D, R>[] = [];
  if (deal) out.push({ kind: 'deal', deal });
  if (low) out.push({ kind: 'low', deal: low });
  if (series) out.push({ kind: 'series', release: series });
  if (newest) out.push({ kind: 'recent', gameId: newest });
  if (anticipated) out.push({ kind: 'anticipated', release: anticipated });
  if (backlog.length) out.push({ kind: 'backlog', gameId: backlog[day % backlog.length] });
  return out;
}

/** Whether a deal's art is ITAD's tall box art (a game without a banner), which is shown whole
 *  rather than cropped to a banner's shape. */
export function isBoxArt(url: string): boolean {
  return /\/boxart\.[a-z]+(\?|$)/i.test(url);
}

/** The header of a game's Steam page on Steam's public CDN, "" without a Steam link: the hero's
 *  wide art for a game whose sheet has no screenshot yet. Loaded through the image proxy. */
export function steamArt(links: Record<string, string>): string {
  const app = links.steam ?? '';
  return /^[0-9]{1,10}$/.test(app) ? `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${app}/header.jpg` : '';
}

/** Below this many pixels a row's overflow is rounding, not more cards. */
const SLACK = 2;

/** Which of a row's arrows to show: none when the cards fit, and each one only while there is
 *  more on its side. */
export function railArrows(el: { scrollLeft: number; scrollWidth: number; clientWidth: number }): { prev: boolean; next: boolean } {
  const max = el.scrollWidth - el.clientWidth;
  if (max <= SLACK) return { prev: false, next: false };
  return { prev: el.scrollLeft > SLACK, next: el.scrollLeft < max - SLACK };
}

/** Whether the user may refresh a block now. */
export function canRefresh(refreshableAt: Date | null, now: Date): boolean {
  return !refreshableAt || refreshableAt.getTime() <= now.getTime();
}

/** How long until a disabled Refresh may be pressed again, in ms; null when it may be now. */
export function refreshDelay(refreshableAt: Date | null, now: Date): number | null {
  if (!refreshableAt || refreshableAt.getTime() <= now.getTime()) return null;
  return refreshableAt.getTime() - now.getTime();
}

/** Whole minutes until `at`, rounded up and at least 1: what "try again in N minutes" says. */
export function minutesUntil(at: Date | null, now: Date): number {
  const ms = at ? at.getTime() - now.getTime() : 0;
  return Math.max(1, Math.ceil(ms / 60_000));
}

/** The address a deal or release card links to: the service's own, unaltered (ITAD's terms forbid
 *  changing its links), and only when it is https; anything else leaves the card without a link. */
export function httpsLink(url: string): string {
  try {
    return new URL(url).protocol === 'https:' ? url : '';
  } catch {
    return '';
  }
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
