import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readAmount } from '../src/lib/money.ts';

test('an amount typed for a currency is read in its minor units', () => {
  assert.equal(readAmount('29,95', 'EUR'), 2995n);
  assert.equal(readAmount('1.234,5', 'EUR'), 123450n);
  assert.equal(readAmount('3000', 'JPY'), 3000n);
  assert.equal(readAmount('0', 'EUR'), 0n, 'zero is an amount');
});

test('empty text is no amount, and text that is not one is refused', () => {
  assert.equal(readAmount('', 'EUR'), undefined);
  assert.equal(readAmount('  ', 'EUR'), undefined);
  assert.equal(readAmount('29,95', 'JPY'), null, 'decimals a currency does not have');
  assert.equal(readAmount('abc', 'EUR'), null);
});
