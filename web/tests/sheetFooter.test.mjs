import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { sheetFooterParts } from '../src/lib/sheetFooter.ts';

const t = (key, opts) => `${key}${opts ? ' ' + JSON.stringify(opts) : ''}`;

test('the footer joins only the parts it has', () => {
  assert.deepEqual(sheetFooterParts({ sources: [], notFoundBy: [] }, undefined, t), []);
  assert.deepEqual(sheetFooterParts({ sources: [], notFoundBy: ['IGDB'] }, undefined, t), [
    'details.notFoundBy {"providers":"IGDB"}',
  ]);
  assert.deepEqual(sheetFooterParts({ sources: ['Steam', 'IGDB'], notFoundBy: ['Wikipedia', 'TheGamesDB'] }, 'today', t), [
    'details.sources {"sources":"Steam + IGDB"}',
    'details.notFoundBy {"providers":"Wikipedia, TheGamesDB"}',
    'details.updated {"date":"today"}',
  ]);
});

test('"not found by" covers no result and an ambiguous one, in every language', () => {
  const read = (lang) => JSON.parse(readFileSync(new URL(`../src/i18n/locales/${lang}.json`, import.meta.url))).details.notFoundBy;
  assert.equal(read('en'), 'No clear match on {{providers}}');
  assert.equal(read('es'), 'Sin coincidencia clara en {{providers}}');
});
