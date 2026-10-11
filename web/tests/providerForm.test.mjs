import assert from 'node:assert/strict';
import { test } from 'node:test';
import { initialEnabled } from '../src/lib/providerForm.ts';

test('configuring a disabled provider keeps it disabled; an enabled one stays enabled', () => {
  assert.equal(initialEnabled(false, 'configure'), false);
  assert.equal(initialEnabled(true, 'configure'), true);
});

test('switching on a provider that needs its settings first opens the dialog ready to enable it', () => {
  assert.equal(initialEnabled(false, 'enable'), true);
});
