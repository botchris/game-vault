// The scanning session's list: pure functions over plain rows, which the page keeps in
// localStorage. Nothing here imports generated code, so Node tests run it as is.
import { normalizeBarcode, validBarcode } from '../../lib/barcode.ts';

const FORMAT = 1;

export type Phase = 'looking' | 'done' | 'error';
export type Status = 'looking' | 'ready' | 'owned' | 'review' | 'error';

/** The parts of an IdentifyBarcode answer a row keeps (plain data, so it can be stored). */
export interface Answer {
  barcode: string;
  owned: { gameId: string; title: string; platform: string }[];
  match: { raw: string; title: string; platform: string; edition: string; providerId: string } | null;
  suggestions: { title: string; platform: string; coverUrl: string; thumbUrl: string; label: string }[];
  existing: { id: string; title: string }[];
  warnings: string[];
}

/** What a row adds: the game (gameId, or a new one with title and cover) and the copy's platform and edition. */
export interface Choice {
  title: string;
  platform: string;
  edition: string;
  coverUrl: string;
  thumbUrl: string;
  gameId: string;
}

/** One scanned box. count is the copies to add: 1 for a new box, 0 for one you already have (until "+1"). */
export interface Row {
  id: string;
  code: string;
  count: number;
  phase: Phase;
  answer?: Answer;
  /** Set when the user chose in the row's detail; otherwise the row resolves by itself. */
  choice?: Choice;
  error?: string;
}

/** One copy to send (AddScannedCopies item, before the batch defaults). */
export interface SendItem {
  clientId: string;
  rowId: string;
  gameId: string;
  title: string;
  coverUrl: string;
  platform: string;
  edition: string;
  barcode: string;
}

/** A removed row and where it was, to undo. */
export interface Removed {
  row: Row;
  index: number;
}

const update = (rows: Row[], id: string, fn: (r: Row) => Row) => rows.map((r) => (r.id === id ? fn(r) : r));

/** The row without its error (dropped, not set to undefined, so a stored row reads back the same). */
const withoutError = ({ error: _error, ...r }: Row): Row => r;

/** Adds a scanned code at the top. A code already in the list (in any form) is a repeat. */
export function addCode(rows: Row[], raw: string, id: string): { rows: Row[]; outcome: 'added' | 'repeat' | 'invalid'; row?: Row } {
  if (!validBarcode(raw)) return { rows, outcome: 'invalid' };
  const code = normalizeBarcode(raw);
  const same = rows.find((r) => r.code === code);
  if (same) return { rows, outcome: 'repeat', row: same };
  const row: Row = { id, code, count: 1, phase: 'looking' };
  return { rows: [row, ...rows], outcome: 'added', row };
}

/** Records a lookup's answer, or its error. A box you already have is not added unless "+1". */
export function settle(rows: Row[], id: string, answer?: Answer, error?: string): Row[] {
  return update(rows, id, (r) => {
    if (error !== undefined || !answer) return { ...r, phase: 'error', error: error ?? 'no answer' };
    const count = answer.owned.length ? Math.max(0, r.count - 1) : r.count;
    return { ...withoutError(r), phase: 'done', answer, count };
  });
}

/** Looks a failed row up again. */
export function retry(rows: Row[], id: string): Row[] {
  return update(rows, id, (r) => ({ ...withoutError(r), phase: 'looking' }));
}

/** One more copy of the same box. */
export function plusOne(rows: Row[], id: string): Row[] {
  return update(rows, id, (r) => ({ ...r, count: r.count + 1 }));
}

/** The user's choice for a row (from its detail). */
export function choose(rows: Row[], id: string, choice: Choice): Row[] {
  return update(rows, id, (r) => ({ ...r, choice }));
}

/**
 * Changes some of a row's choice, on top of what it is now (the user's choice, or what the row
 * resolved by itself). Edits are applied to the current row, so quick successive edits all stay;
 * the batch platform is never written into the row.
 */
export function amend(rows: Row[], id: string, patch: Partial<Choice>): Row[] {
  return update(rows, id, (r) => {
    const base = r.choice ?? resolved(r, '');
    if (!base) return r;
    return { ...r, choice: { ...base, ...patch } };
  });
}

export function remove(rows: Row[], id: string): { rows: Row[]; removed?: Removed } {
  const index = rows.findIndex((r) => r.id === id);
  if (index < 0) return { rows };
  return { rows: rows.filter((r) => r.id !== id), removed: { row: rows[index]!, index } };
}

export function restore(rows: Row[], removed?: Removed): Row[] {
  if (!removed || rows.some((r) => r.id === removed.row.id)) return rows;
  const out = [...rows];
  out.splice(Math.min(removed.index, out.length), 0, removed.row);
  return out;
}

/** What the row adds: the user's choice, or what the answer suggests; the batch platform wins. */
export function resolved(row: Row, batchPlatform: string): Choice | null {
  const a = row.answer;
  if (!a) return null;
  const owned = a.owned[0];
  if (owned) {
    return { title: owned.title, platform: batchPlatform || owned.platform, edition: '', coverUrl: '', thumbUrl: '', gameId: owned.gameId };
  }
  const s = a.suggestions[0];
  const base: Choice = row.choice ?? {
    // A product name without platform is usually not a game: no title then.
    title: s?.title ?? (a.match?.platform ? a.match.title : ''),
    platform: a.match?.platform || s?.platform || '',
    edition: a.match?.edition ?? '',
    coverUrl: s?.coverUrl ?? '',
    thumbUrl: s?.thumbUrl ?? '',
    gameId: a.existing.length === 1 ? a.existing[0]!.id : '',
  };
  return { ...base, platform: batchPlatform || base.platform };
}

export function status(row: Row, batchPlatform: string): Status {
  if (row.phase === 'looking') return 'looking';
  if (row.phase === 'error' || !row.answer) return 'error';
  if (row.answer.owned.length) return 'owned';
  const c = resolved(row, batchPlatform)!;
  if (!c.title.trim() || !c.platform.trim()) return 'review';
  // Resolved by itself only when there is a suggested game and at most one of yours to add it to.
  if (!row.choice && (row.answer.suggestions.length === 0 || row.answer.existing.length > 1)) return 'review';
  return 'ready';
}

export function summary(rows: Row[], batchPlatform: string) {
  const out = { ready: 0, review: 0, owned: 0, looking: 0, error: 0, copies: 0 };
  for (const r of rows) out[status(r, batchPlatform)]++;
  out.copies = sendItems(rows, batchPlatform).length;
  return out;
}

/** One item per copy of every Ready row, and of "you have it" rows with "+1". */
export function sendItems(rows: Row[], batchPlatform: string): SendItem[] {
  const items: SendItem[] = [];
  for (const r of rows) {
    const st = status(r, batchPlatform);
    if (st !== 'ready' && st !== 'owned') continue;
    const c = resolved(r, batchPlatform)!;
    for (let n = 0; n < r.count; n++) {
      items.push({
        clientId: `${r.id}:${n}`, rowId: r.id, gameId: c.gameId, title: c.title.trim(), coverUrl: c.coverUrl,
        platform: c.platform.trim(), edition: c.edition.trim(), barcode: r.code,
      });
    }
  }
  return items;
}

/**
 * Applies AddScannedCopies results: saved copies leave their row (the row leaves when none is
 * left), failed ones stay with the error. saved lists each row with a saved copy and its game.
 */
export function applyResults(rows: Row[], results: { clientId: string; gameId: string; error: string }[]) {
  const saved: { row: Row; gameId: string }[] = [];
  const next: Row[] = [];
  for (const r of rows) {
    const mine = results.filter((x) => x.clientId.startsWith(`${r.id}:`));
    if (!mine.length) {
      next.push(r);
      continue;
    }
    const ok = mine.filter((x) => !x.error);
    const failed = mine.filter((x) => x.error);
    if (ok.length) saved.push({ row: r, gameId: ok[0]!.gameId });
    if (failed.length) next.push({ ...r, count: failed.length, error: failed[0]!.error });
  }
  return { rows: next, saved };
}

export function saveRows(rows: Row[]): string {
  return JSON.stringify({ v: FORMAT, rows });
}

const isRow = (r: unknown): r is Row => {
  const x = r as Row;
  return !!x && typeof x.id === 'string' && typeof x.code === 'string' && typeof x.count === 'number'
    && ['looking', 'done', 'error'].includes(x.phase) && (x.phase !== 'done' || !!x.answer);
};

/** The stored list; empty when there is none, it is broken or it has another format. */
export function loadRows(text: string | null): Row[] {
  if (!text) return [];
  try {
    const data = JSON.parse(text) as { v?: number; rows?: unknown[] };
    if (data.v !== FORMAT || !Array.isArray(data.rows)) return [];
    return data.rows.filter(isRow);
  } catch {
    return [];
  }
}
