import { timestampDate, type Timestamp } from '@bufbuild/protobuf/wkt';
import { CopyContent, CopyGrade, CopyKind, CopyStatus, PlayStatus, type Copy, type CopyDetails, type Game } from '../gen/gamevault/v1/game_pb';

export { CopyKind, CopyStatus, PlayStatus };
export type { Copy, CopyDetails, Game };

/** Plain, editable shape of CopyDetails (without the protobuf $typeName). */
export type CopyDetailsInput = Omit<CopyDetails, '$typeName'>;

export const KINDS = [CopyKind.KEY, CopyKind.LIBRARY, CopyKind.PHYSICAL] as const;

/** Translation key suffix for each kind: t(`kind.${kindKey(k)}`). */
export function kindKey(k: CopyKind): string {
  return CopyKind[k].toLowerCase();
}

export function statusKey(s: CopyStatus): string {
  return CopyStatus[s].toLowerCase();
}

/** Statuses allowed for each kind; the first one is the default. Mirrors the domain rules. */
export const STATUSES_BY_KIND: Record<CopyKind, CopyStatus[]> = {
  [CopyKind.UNSPECIFIED]: [],
  [CopyKind.KEY]: [CopyStatus.UNREVEALED, CopyStatus.REVEALED, CopyStatus.REDEEMED, CopyStatus.GIFTED, CopyStatus.EXPIRED],
  [CopyKind.LIBRARY]: [CopyStatus.OWNED],
  [CopyKind.PHYSICAL]: [CopyStatus.OWNED, CopyStatus.LENT, CopyStatus.SOLD],
};

export const STORE_PLATFORMS = [
  'Steam', 'Epic Games', 'GOG', 'EA App', 'Battle.net', 'Ubisoft Connect', 'Microsoft Store / Xbox',
  'Rockstar', 'Battlestate (Tarkov)', 'Riot', 'itch.io', 'Amazon Games', 'PlayStation Store', 'Nintendo eShop',
];

export const PHYSICAL_PLATFORMS = [
  'PC', 'PS5', 'PS4', 'PS3', 'PS2', 'PS1', 'PSP', 'PS Vita', 'Xbox Series', 'Xbox One', 'Xbox 360', 'Xbox',
  'Switch', 'Wii U', 'Wii', 'GameCube', 'N64', '3DS', 'DS', 'Game Boy',
];

export function emptyDetails(kind: CopyKind = CopyKind.PHYSICAL): CopyDetailsInput {
  return {
    kind, platform: '', status: STATUSES_BY_KIND[kind][0], key: '', redeemBy: '', origin: '',
    acquiredOn: '', edition: '', grade: CopyGrade.UNSPECIFIED, contents: [], location: '', notes: '', barcode: '', fields: {},
  };
}

/** Play statuses in the order the user goes through them; UNSPECIFIED (not said) is left out. */
export const PLAY_STATUSES = [PlayStatus.BACKLOG, PlayStatus.PLAYING, PlayStatus.FINISHED, PlayStatus.ABANDONED] as const;

/** Translation key suffix: t(`play.${playKey(s)}`); UNSPECIFIED is "unspecified". */
export const playKey = (s: PlayStatus) => PlayStatus[s].toLowerCase();

/** The highest rating, in stars; 0 means unrated. */
export const MAX_RATING = 5;

/** Every editable field of a game, as UpdateGame expects them: it replaces all of them, so a
 *  change starts from this and overrides only what it changes. */
export const gameInfo = (g: Game) => ({
  id: g.id, title: g.title, links: g.links, notes: g.notes, coverUrl: g.coverUrl, playStatus: g.playStatus, rating: g.rating, fields: g.fields,
});

export const GRADES = [CopyGrade.SEALED, CopyGrade.MINT, CopyGrade.VERY_GOOD, CopyGrade.GOOD, CopyGrade.ACCEPTABLE, CopyGrade.DAMAGED] as const;
export const CONTENTS = [CopyContent.BOX, CopyContent.MANUAL, CopyContent.MEDIA, CopyContent.EXTRAS] as const;

/** Translation key suffixes: t(`grade.${gradeKey(g)}`), t(`content.${contentKey(c)}`). */
export const gradeKey = (g: CopyGrade) => CopyGrade[g].toLowerCase();
export const contentKey = (c: CopyContent) => CopyContent[c].toLowerCase();

/** The distinct places copies are kept in, sorted, for the location suggestions. */
export function usedLocations(games: Game[]): string[] {
  const set = new Set<string>();
  for (const g of games) for (const c of g.copies) if (c.details?.location) set.add(c.details.location);
  return [...set].sort((a, b) => a.localeCompare(b));
}

/** Pending = a key you still have to reveal or redeem. */
export const isPendingKey = (c: Copy) =>
  c.details?.kind === CopyKind.KEY &&
  (c.details.status === CopyStatus.UNREVEALED || c.details.status === CopyStatus.REVEALED);

/** Whole days from today until a YYYY-MM-DD date; null when there is no date. */
export function daysUntil(date: string): number | null {
  if (!date) return null;
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  return Math.round((new Date(`${date}T00:00:00`).getTime() - today.getTime()) / 86_400_000);
}

/** Earliest redeem-by date among the game's pending keys, or ''. */
export function nextDeadline(g: Game): string {
  return g.copies
    .filter((c) => isPendingKey(c) && c.details?.redeemBy)
    .map((c) => c.details!.redeemBy)
    .sort()[0] ?? '';
}

export function toDate(ts?: Timestamp): Date | null {
  return ts ? timestampDate(ts) : null;
}

/** Saves bytes as a file in the browser. */
export function downloadBytes(name: string, bytes: Uint8Array, type: string) {
  const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type }));
  const a = Object.assign(document.createElement('a'), { href: url, download: name });
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * URL of the store page that redeems a key, with the key pre-filled, or null when the store has
 * no such page. Only keys still pending (revealed, not redeemed) get one.
 */
export function redeemUrl(c: Copy): string | null {
  const d = c.details;
  if (!d || d.kind !== CopyKind.KEY || d.status !== CopyStatus.REVEALED || !d.key || d.key.startsWith('http')) return null;
  const page = REDEEM_PAGES[d.platform];
  return page ? page + encodeURIComponent(d.key) : null;
}

/** Stores whose redeem page takes the key in its URL, by platform name. */
const REDEEM_PAGES: Record<string, string> = {
  'Steam': 'https://store.steampowered.com/account/registerkey?key=',
  'GOG': 'https://www.gog.com/redeem/',
};

export { validBarcode } from './barcode';
