import assert from 'node:assert/strict';
import { test } from 'node:test';
import { httpsUrl, isAutoLink, rebaseRecord } from '../src/lib/links.ts';

test('only https addresses from providers become links', () => {
  assert.equal(httpsUrl('https://en.wikipedia.org/wiki/Portal_2'), 'https://en.wikipedia.org/wiki/Portal_2');
  assert.equal(httpsUrl('http://en.wikipedia.org/wiki/Portal_2'), undefined);
  assert.equal(httpsUrl('javascript:alert(1)'), undefined);
  assert.equal(httpsUrl('//evil.test/x'), undefined);
  assert.equal(httpsUrl('not a url'), undefined);
  assert.equal(httpsUrl(''), undefined);
  assert.equal(httpsUrl(undefined), undefined);
});

test('the automatic badge stays only while the link is the one found', () => {
  const g = { links: { steam: '100', psn: 'P1' }, autoLinks: ['steam'] };
  assert.equal(isAutoLink(g, 'steam', '100'), true);
  assert.equal(isAutoLink(g, 'steam', ' 100 '), true);
  assert.equal(isAutoLink(g, 'steam', '101'), false, 'changed by the user: theirs');
  assert.equal(isAutoLink(g, 'steam', ''), false, 'cleared: it will be rejected');
  assert.equal(isAutoLink(g, 'psn', 'P1'), false, 'imported, not automatic');
});

test('a form keeps only what the user changed and takes everything else from the game as it is now', () => {
  const loaded = { steam: '', gog: 'G1', psn: 'P1' };
  const edited = { steam: '', gog: 'G2', psn: '' };
  const current = { steam: '100', gog: 'G1', psn: 'P1', epic: 'E1' };
  assert.deepEqual(rebaseRecord(loaded, edited, current), { steam: '100', gog: 'G2', psn: '', epic: 'E1' },
    'the automatic Steam link found meanwhile and the new Epic one stay; the edits win; a cleared link stays cleared');
  assert.deepEqual(rebaseRecord({ steam: '100' }, { steam: ' 100 ' }, {}, (a, b) => a.trim() === b.trim()), {},
    'unchanged except for spaces: the game as it is now (the link was removed meanwhile)');
  assert.deepEqual(rebaseRecord({}, { gog: 'G9' }, { gog: 'G1' }), { gog: 'G9' }, 'a link the user added wins');
});
