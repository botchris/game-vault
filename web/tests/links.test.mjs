import assert from 'node:assert/strict';
import { test } from 'node:test';
import { httpsUrl } from '../src/lib/links.ts';

test('only https addresses from providers become links', () => {
  assert.equal(httpsUrl('https://en.wikipedia.org/wiki/Portal_2'), 'https://en.wikipedia.org/wiki/Portal_2');
  assert.equal(httpsUrl('http://en.wikipedia.org/wiki/Portal_2'), undefined);
  assert.equal(httpsUrl('javascript:alert(1)'), undefined);
  assert.equal(httpsUrl('//evil.test/x'), undefined);
  assert.equal(httpsUrl('not a url'), undefined);
  assert.equal(httpsUrl(''), undefined);
  assert.equal(httpsUrl(undefined), undefined);
});
