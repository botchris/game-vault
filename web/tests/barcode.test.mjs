import assert from 'node:assert/strict';
import { test } from 'node:test';
import { lookupLinks } from '../src/lib/barcode.ts';

test('the manual lookup links carry the normalized code', () => {
  assert.deepEqual(lookupLinks('045496590246'), {
    eanSearch: 'https://www.ean-search.org/?q=0045496590246',
    web: 'https://www.google.com/search?q=%220045496590246%22',
  });
  assert.equal(lookupLinks('5030934110075').eanSearch, 'https://www.ean-search.org/?q=5030934110075');
});
