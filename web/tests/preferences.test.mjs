import assert from 'node:assert/strict';
import { test } from 'node:test';
import { currencyView } from '../src/lib/preferences.ts';

test('a saved currency is shown as saved', () => {
  assert.deepEqual(currencyView({ loaded: true, currency: 'JPY' }, 'EUR'), { currency: 'JPY', saved: true, unknown: false, error: '' });
});

test("without a saved currency the region's is proposed, to be saved", () => {
  assert.deepEqual(currencyView({ loaded: true, currency: '' }, 'EUR'), { currency: 'EUR', saved: false, unknown: false, error: '' });
});

test('preferences that failed to load are unknown, never "not saved": saving the proposal could overwrite the saved one', () => {
  assert.deepEqual(currencyView({ loaded: false, error: 'unavailable' }, 'EUR'), { currency: 'EUR', saved: false, unknown: true, error: 'unavailable' });
  assert.equal(currencyView({ loaded: false, error: '' }, 'EUR').unknown, true, 'not loaded yet');
});
