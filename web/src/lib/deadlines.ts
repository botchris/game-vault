// Which keys are pending and how urgent their redeem deadlines are: the library's list, its
// "Expiring" filter, the copy cards of the game sheet and the home page all read it from here, so
// they never disagree. Structural types only, so Node tests import it (tests/deadlines.test.mjs).

// The API's numbers (proto/gamevault/v1/game.proto): CopyKind.KEY, CopyStatus.UNREVEALED, REVEALED.
const KIND_KEY = 1;
const STATUS_UNREVEALED = 1;
const STATUS_REVEALED = 2;

const DAY = 86_400_000;

/** Pending = a key you still have to reveal or redeem. */
export const isPendingKey = (c: { details?: { kind: number; status: number } }) =>
  c.details?.kind === KIND_KEY &&
  (c.details.status === STATUS_UNREVEALED || c.details.status === STATUS_REVEALED);

/** Whole days from `today` (its local date) until a YYYY-MM-DD date, negative once it has passed;
 *  null when there is no date. */
export function daysUntil(date: string, today: Date = new Date()): number | null {
  if (!date) return null;
  const start = new Date(today);
  start.setHours(0, 0, 0, 0);
  return Math.round((new Date(`${date}T00:00:00`).getTime() - start.getTime()) / DAY);
}

/** Days left at which a deadline is urgent: the "Expiring" filter, shown as danger. */
export const EXPIRING_DAYS = 30;

/** Days left at which a deadline is worth a look, shown as a warning. */
export const SOON_DAYS = 90;

/** The tone of a deadline `days` away: danger, warn, or null when there is time. */
export function deadlineTone(days: number): 'danger' | 'warn' | null {
  if (days <= EXPIRING_DAYS) return 'danger';
  if (days <= SOON_DAYS) return 'warn';
  return null;
}

/** Whether a deadline `days` away (null: none) counts as expiring. */
export function expiresSoon(days: number | null): boolean {
  return days !== null && deadlineTone(days) === 'danger';
}
