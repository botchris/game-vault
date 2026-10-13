import assert from 'node:assert/strict';
import { test } from 'node:test';
import { MIN_PASSWORD_CHARS, passwordLongEnough } from '../src/lib/password.ts';

test('the minimum is counted in characters, as the server counts them', () => {
  assert.equal(MIN_PASSWORD_CHARS, 8);
  assert.equal(passwordLongEnough('1234567'), false);
  assert.equal(passwordLongEnough('12345678'), true);
});

test('an emoji is one character, not two UTF-16 units', () => {
  // Four emoji are 8 UTF-16 units but 4 characters: the browser's minLength would let them pass.
  assert.equal('😀😀😀😀'.length, 8);
  assert.equal(passwordLongEnough('😀😀😀😀'), false);
  assert.equal(passwordLongEnough('😀😀😀😀😀😀😀😀'), true);
  assert.equal(passwordLongEnough('abcdefg😀'), true);
});
