import assert from 'node:assert/strict';
import { test } from 'node:test';
import { EXPIRING_DAYS, SOON_DAYS, daysUntil, deadlineTone, expiresSoon, isPendingKey } from '../src/lib/deadlines.ts';

test('a redeem deadline is urgent within a month, worth a look within three, and quiet after', () => {
  assert.equal(deadlineTone(-1), 'danger', 'already past');
  assert.equal(deadlineTone(0), 'danger');
  assert.equal(deadlineTone(EXPIRING_DAYS), 'danger');
  assert.equal(deadlineTone(EXPIRING_DAYS + 1), 'warn');
  assert.equal(deadlineTone(SOON_DAYS), 'warn');
  assert.equal(deadlineTone(SOON_DAYS + 1), null);
});

test('"Expiring" in the library is the urgent tone, so the list and the sheet agree', () => {
  assert.equal(expiresSoon(EXPIRING_DAYS), true);
  assert.equal(expiresSoon(EXPIRING_DAYS + 1), false);
  assert.equal(expiresSoon(null), false, 'no deadline');
});

test('a pending key is one still to reveal or redeem', () => {
  const copy = (kind, status) => ({ details: { kind, status } });
  assert.equal(isPendingKey(copy(1, 1)), true, 'unrevealed key');
  assert.equal(isPendingKey(copy(1, 2)), true, 'revealed key');
  assert.equal(isPendingKey(copy(1, 3)), false, 'redeemed key');
  assert.equal(isPendingKey(copy(1, 5)), false, 'expired key');
  assert.equal(isPendingKey(copy(2, 1)), false, 'not a key');
  assert.equal(isPendingKey({}), false, 'no details');
});

test('a deadline is whole days from the local date, negative once past, null when there is none', () => {
  const today = new Date(2026, 9, 15, 23, 30); // late in the day: still the 15th
  assert.equal(daysUntil('2026-10-15', today), 0);
  assert.equal(daysUntil('2026-10-18', today), 3);
  assert.equal(daysUntil('2026-10-12', today), -3);
  assert.equal(daysUntil('', today), null);
  assert.equal(typeof daysUntil('2026-10-15'), 'number', 'today by default');
});
