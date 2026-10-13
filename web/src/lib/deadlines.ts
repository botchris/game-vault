// How urgent a key's redeem deadline is: the library's list, its "Expiring" filter and the copy
// cards of the game sheet all read it from here, so they never disagree.

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
