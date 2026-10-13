import assert from 'node:assert/strict';
import { test } from 'node:test';
import { consentMissing } from '../src/lib/settingsForm.ts';

test('a required risk not ticked blocks testing and saving; an optional one does not', () => {
  const required = { key: 'risk', required: true };
  const optional = { key: 'extra', required: false };
  assert.equal(consentMissing([required], {}), true);
  assert.equal(consentMissing([required], { risk: '' }), true);
  assert.equal(consentMissing([required], { risk: 'yes' }), false);
  assert.equal(consentMissing([optional], {}), false);
  assert.equal(consentMissing([], {}), false, 'nothing to accept');
});
