import assert from 'node:assert/strict';
import { test } from 'node:test';
import { EXPIRING_DAYS, SOON_DAYS, deadlineTone, expiresSoon } from '../src/lib/deadlines.ts';

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
