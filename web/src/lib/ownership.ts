// The bundle checker's pure parts: a pasted list cleaned into titles, split into requests, and the
// answers turned into rows. Structural types only, so Node tests import this file as it is.

/** Most titles one request may hold (the server's limit). */
export const MAX_CHECK_ITEMS = 200;

/** Longest line taken as a title: longer lines are prose, not titles. */
const MAX_TITLE = 200;

const BULLET = /^(?:[-–—*•·●▪►>+]\s*)+/;
// Only list forms: "#1 Fan" or "7 Nights" start a title, "#1." and "1)" number a list.
const NUMBERING = /^(?:\(\d{1,3}\)|\d{1,3}[.)]|#\d{1,3}[.):])\s+/;
const PRICE = String.raw`(?:(?:US|CA|AU|NZ)?[$€£¥]\s?\d+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?\s?(?:[$€£¥]|USD|EUR|GBP))`;
const LEADING_PRICE = new RegExp(String.raw`^${PRICE}\s+`, 'i');
const TRAILING_PRICE = new RegExp(String.raw`\s*(?:[-–—|:]\s*)?${PRICE}$`, 'i');
const TRAILING_DISCOUNT = /\s+-\d{1,3}\s?%$/;
const LONE_CURRENCY = /(^|\s)[$€£¥](?=\s|$)/g;
const DANGLING = /[\s\-–—|:,;]+$/;

/** Lines marked as add-ons rather than games: "[DLC]", "(Soundtrack)", "DLC: …", "… - Soundtrack",
 *  "… Soundtrack", "… OST". A marker word elsewhere ("DLC Hunters", "Soundtrack Hero") is part of a
 *  title. */
const ADD_ON = [
  /[([]\s*(?:dlc|(?:original\s+)?soundtrack|ost)\s*[)\]]/i,
  /^(?:dlc|soundtrack|ost)(?:\s*:|\s+[-–—])\s*/i,
  /\s[-–—:]\s*(?:dlc|(?:original\s+)?soundtrack|ost)$/i,
  /\s(?:original\s+)?soundtrack$/i,
  /\s(?:DLC|OST)$/,
];

export interface CleanedList {
  titles: string[];
  /** Lines left out because they name an add-on (DLC, soundtrack), not a game. */
  skipped: number;
  /** Lines left out for being too long to be a title. */
  tooLong: number;
}

/** The cell of a table line that holds the title: the first with letters, so rank, number and price
 *  cells before it are passed over. A line without tabs is its own cell. */
function titleCell(line: string): string {
  if (!line.includes('\t')) return line;
  const cells = line.split('\t').map((c) => c.trim()).filter(Boolean);
  return cells.find((c) => /\p{L}/u.test(c)) ?? cells[0] ?? '';
}

function cleanLine(line: string): string {
  let s = line.replace(/\s+/g, ' ').trim().replace(BULLET, '').replace(NUMBERING, '').replace(LEADING_PRICE, '');
  // "Mirror Run -75% $4.99": a price, then a discount, then whatever separator they leave.
  for (let before = ''; before !== s;) {
    before = s;
    s = s.replace(TRAILING_PRICE, '').replace(TRAILING_DISCOUNT, '').replace(DANGLING, '').trim();
  }
  return s.replace(LONE_CURRENCY, '$1').replace(/\s+/g, ' ').trim();
}

/** Cleans pasted text into one title per line: bullets, numbering, prices, discounts and currency
 *  signs go; add-on lines are skipped (and counted); blank and duplicate lines are dropped,
 *  overlong ones dropped and counted. A line copied from a table keeps its first cell with letters. */
export function cleanTitles(text: string): CleanedList {
  const titles: string[] = [];
  const seen = new Set<string>();
  let skipped = 0;
  let tooLong = 0;
  for (const raw of text.split(/\r?\n/)) {
    const title = cleanLine(titleCell(raw));
    if (!title) continue;
    if (title.length > MAX_TITLE) {
      tooLong++;
      continue;
    }
    if (ADD_ON.some((re) => re.test(title))) {
      skipped++;
      continue;
    }
    const key = title.normalize('NFKC').toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    titles.push(title);
  }
  return { titles, skipped, tooLong };
}

/** Splits items into requests of at most `size`, in order. */
export function chunk<T>(items: T[], size = MAX_CHECK_ITEMS): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size));
  return out;
}

export interface CopyLike { platform: string; system: string; kind: string; pendingKey: boolean }
export interface GameLike { id: string; title: string; copies: CopyLike[]; path: string }
export interface ResultLike { status: string; game?: GameLike; candidates: GameLike[] }
export type RowStatus = 'owned' | 'possible' | 'not_owned';
export interface Row { title: string; status: RowStatus; game?: GameLike; candidates: GameLike[] }

/** One row per title, in the order pasted. A missing or unknown answer counts as not owned. */
export function toRows(titles: string[], results: ResultLike[]): Row[] {
  return titles.map((title, i) => {
    const r = results[i];
    if (r?.status === 'owned' && r.game) return { title, status: 'owned', game: r.game, candidates: [] };
    if (r?.status === 'possible') return { title, status: 'possible', candidates: r.candidates };
    return { title, status: 'not_owned', candidates: [] };
  });
}

/** "You own N of M": owned rows of all rows; possible ones are counted apart. */
export function summarize(rows: Row[]): { owned: number; possible: number; total: number } {
  return {
    owned: rows.filter((r) => r.status === 'owned').length,
    possible: rows.filter((r) => r.status === 'possible').length,
    total: rows.length,
  };
}

/** The titles not owned, one per line, for the clipboard. Possible ones may be owned: left out. */
export function notOwnedText(rows: Row[]): string {
  return rows.filter((r) => r.status === 'not_owned').map((r) => r.title).join('\n');
}

/** The server answered for fewer titles than were sent: its answers cannot be matched to titles. */
export class ShortAnswerError extends Error {
  constructor() {
    super('The server answered for fewer titles than were sent.');
    this.name = 'ShortAnswerError';
  }
}

/** Asks for the titles in requests of at most MAX_CHECK_ITEMS and joins the answers into rows. A
 *  failed request (or one answered short) ends the run: the rows of the requests before it are kept,
 *  with the error. */
export async function checkAll(titles: string[], ask: (part: string[]) => Promise<ResultLike[]>): Promise<{ rows: Row[]; error?: unknown }> {
  const results: ResultLike[] = [];
  try {
    for (const part of chunk(titles)) {
      const got = await ask(part);
      if (got.length < part.length) throw new ShortAnswerError();
      results.push(...got);
    }
    return { rows: toRows(titles, results) };
  } catch (error) {
    return { rows: toRows(titles.slice(0, results.length), results), error };
  }
}
