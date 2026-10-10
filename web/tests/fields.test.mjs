import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  chosen, copyValuesFor, fieldText, filterOptions, formatNumber, hasDisplayValue, keysOf, matchesFieldFilters, numberInput, parseNumber,
  pruneFieldFilter, searchableText,
} from '../src/lib/fields.ts';

const fmt = {
  money: (n, c) => `${c} ${n}`,
  date: (d) => `<${d}>`,
  yes: 'Yes', no: 'No', hours: 'h', minutes: 'min', locale: 'en',
};
const def = (over) => ({ id: 'f', name: 'F', type: 'text', scope: 'game', kinds: [], decimals: 0, unit: '', currency: '', choices: [], ...over });
const val = (c, v) => ({ value: { case: c, value: v } });
const choices = [{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Beta' }, { id: 'c', name: 'Gamma' }];
const game = (fields, copies = [], kind = 3) => ({ fields, copies: copies.map((f) => ({ details: { kind, fields: f } })) });

test('fieldText formats every type', () => {
  assert.equal(fieldText(def({ type: 'number', decimals: 2, unit: 'g' }), val('number', 45050n), fmt), '450.50 g');
  assert.equal(fieldText(def({ type: 'duration' }), val('minutes', 750n), fmt), '12 h 30 min');
  assert.equal(fieldText(def({ type: 'bool' }), val('bool', true), fmt), 'Yes');
  assert.equal(fieldText(def({ type: 'bool' }), val('bool', false), fmt), 'No');
  assert.equal(fieldText(def({ type: 'multilist', choices }), val('choices', { ids: ['c', 'a'] }), fmt), 'Alpha, Gamma');
  assert.equal(fieldText(def({ type: 'list', choices }), val('choice', 'b'), fmt), 'Beta');
  assert.equal(fieldText(def({ type: 'date' }), val('date', '2024-02'), fmt), '<2024-02>');
  assert.equal(fieldText(def({ type: 'money' }), val('money', { amountMinor: 1250n, currency: 'EUR' }), fmt), 'EUR 1250');
  assert.equal(fieldText(def(), undefined, fmt), '');
  assert.equal(fieldText(def({ type: 'number' }), val('number', 1234567n), fmt), '1,234,567');
});

test('fieldText writes numbers and durations in the UI language', () => {
  const es = { ...fmt, locale: 'es' };
  assert.equal(fieldText(def({ type: 'number', decimals: 2, unit: 'kg' }), val('number', 125n), es), '1,25 kg');
  assert.equal(fieldText(def({ type: 'number', decimals: 2 }), val('number', -50n), es), '-0,50');
  assert.equal(fieldText(def({ type: 'duration' }), val('minutes', 90n), es), '1 h 30 min');
  assert.equal(fieldText(def({ type: 'duration' }), val('minutes', 0n), es), '0 min');
});

test('formatNumber keeps every digit of large values', () => {
  assert.equal(formatNumber(123456789012345678901n, 2, 'en'), '1,234,567,890,123,456,789.01');
  assert.equal(formatNumber(5n, 2, 'en'), '0.05');
  assert.equal(formatNumber(-5n, 0, 'en'), '-5');
});

test('a list value whose choice was removed has nothing to show', () => {
  const list = def({ type: 'list', choices });
  const multi = def({ type: 'multilist', choices });
  assert.equal(hasDisplayValue(list, val('choice', 'gone')), false);
  assert.equal(fieldText(list, val('choice', 'gone'), fmt), '');
  assert.equal(hasDisplayValue(multi, val('choices', { ids: ['gone'] })), false);
  assert.equal(hasDisplayValue(multi, val('choices', { ids: ['gone', 'b'] })), true);
  assert.deepEqual(chosen(multi, val('choices', { ids: ['c', 'gone', 'a'] })).map((c) => c.id), ['a', 'c']);
  assert.equal(hasDisplayValue(def(), val('text', '  ')), false);
  assert.equal(hasDisplayValue(def({ type: 'bool' }), val('bool', false)), true);
  assert.equal(hasDisplayValue(def(), undefined), false);
});

test('parseNumber accepts a dot or a comma and rejects what does not fit', () => {
  assert.equal(parseNumber('12.5', 2), 1250n);
  assert.equal(parseNumber('12,50', 2), 1250n);
  assert.equal(parseNumber('-3', 2), -300n);
  assert.equal(parseNumber('1.234', 0), null);
  assert.equal(parseNumber('', 2), null);
  assert.equal(parseNumber('12.345', 2), null);
  assert.equal(parseNumber('42', 0), 42n);
});

test('parseNumber edge inputs', () => {
  assert.equal(parseNumber('.5', 2), 50n);
  assert.equal(parseNumber(',5', 2), 50n);
  assert.equal(parseNumber('12.', 2), 1200n);
  assert.equal(parseNumber('12,', 0), 12n);
  assert.equal(parseNumber('12.50', 2), 1250n);
  assert.equal(parseNumber('12,50', 2), 1250n);
  assert.equal(parseNumber('-0.50', 2), -50n);
  assert.equal(parseNumber('-.5', 2), -50n);
  assert.equal(parseNumber(' 7 ', 2), 700n);
  assert.equal(parseNumber('99999999999999999999.99', 2), 9999999999999999999999n);
  for (const bad of ['.', '-', '-.', 'abc', '1a', '1.2.3', '1,234.5', '+3', '1 000', '--1']) assert.equal(parseNumber(bad, 2), null, bad);
  for (const bad of ['.5', '12.5', 'abc']) assert.equal(parseNumber(bad, 0), null, bad);
});

test('numberInput writes hundredths with two decimals', () => {
  assert.equal(numberInput(1250n, 2), '12.50');
  assert.equal(numberInput(-5n, 2), '-0.05');
  assert.equal(numberInput(42n, 0), '42');
  assert.equal(numberInput(1250n, 2, 'es'), '12,50');
  assert.equal(parseNumber(numberInput(-125n, 2, 'es'), 2), -125n);
});

test('searchableText holds only text and long-text values of the game and its copies', () => {
  const defs = [
    def({ id: 't', type: 'text' }),
    def({ id: 'l', type: 'longtext', scope: 'copy' }),
    def({ id: 'n', type: 'number' }),
    def({ id: 'x', type: 'list', choices }),
  ];
  const g = game({ t: val('text', 'hello'), n: val('number', 7n), x: val('choice', 'a') }, [{ l: val('text', 'long note') }]);
  assert.equal(searchableText(g, defs), 'hello long note');
});

test('matchesFieldFilters', () => {
  const list = def({ id: 'x', type: 'list', choices });
  const flag = def({ id: 'b', type: 'bool' });
  const copy = def({ id: 'k', type: 'list', scope: 'copy', choices });
  const g = game({ x: val('choice', 'a'), b: val('bool', true) }, [{ k: val('choice', 'b') }, {}]);
  const empty = game({}, []);
  assert.equal(matchesFieldFilters(g, [list], { x: ['a'] }), true);
  assert.equal(matchesFieldFilters(g, [list], { x: ['b', 'c'] }), false);
  assert.equal(matchesFieldFilters(empty, [list], { x: [''] }), true);
  assert.equal(matchesFieldFilters(g, [list], { x: [''] }), false);
  assert.equal(matchesFieldFilters(g, [flag], { b: ['yes'] }), true);
  assert.equal(matchesFieldFilters(g, [flag], { b: ['no'] }), false);
  assert.equal(matchesFieldFilters(empty, [flag], { b: [''] }), true);
  assert.equal(matchesFieldFilters(g, [copy], { k: ['b'] }), true);
  assert.equal(matchesFieldFilters(g, [copy], { k: ['c'] }), false);
  assert.equal(matchesFieldFilters(g, [list, copy], { x: [], k: [] }), true);
  assert.equal(matchesFieldFilters(g, [list], {}), true);
});

test('matchesFieldFilters looks only at copies of the kinds a copy field applies to', () => {
  const disc = def({ id: 'k', type: 'list', scope: 'copy', kinds: [3], choices });
  const keyCopy = game({}, [{ k: val('choice', 'a') }], 1); // a key (kind 1) holding a stale value
  assert.equal(matchesFieldFilters(keyCopy, [disc], { k: ['a'] }), false);
  assert.equal(matchesFieldFilters(keyCopy, [disc], { k: [''] }), true); // no copy of the kind: no value
  const physical = game({}, [{ k: val('choice', 'a') }], 3);
  assert.equal(matchesFieldFilters(physical, [disc], { k: ['a'] }), true);
});

test('filterOptions counts games per choice and for no value', () => {
  const list = def({ id: 'x', type: 'list', choices });
  const games = [game({ x: val('choice', 'a') }), game({ x: val('choice', 'a') }), game({ x: val('choice', 'b') }), game({})];
  assert.deepEqual(filterOptions(games, list), [
    { key: 'a', count: 2 }, { key: 'b', count: 1 }, { key: 'c', count: 0 }, { key: '', count: 1 },
  ]);
  const flag = def({ id: 'b', type: 'bool' });
  assert.deepEqual(filterOptions([game({ b: val('bool', true) }), game({})], flag), [
    { key: 'yes', count: 1 }, { key: 'no', count: 0 }, { key: '', count: 1 },
  ]);
});

test('fields that are not filterable have no options and their values match no key', () => {
  for (const type of ['text', 'longtext', 'number', 'money', 'date', 'duration']) {
    assert.deepEqual(filterOptions([game({ f: val('text', 'x') }), game({})], def({ type })), [], type);
  }
  assert.deepEqual(keysOf(val('text', 'x')), []);
  assert.deepEqual(keysOf(val('number', 3n)), []);
  assert.deepEqual(keysOf(val('minutes', 3n)), []);
  assert.deepEqual(keysOf(undefined), ['']);
  assert.deepEqual(keysOf(val('choices', { ids: [] })), ['']);
});

test('pruneFieldFilter drops deleted fields, removed choices and fields that cannot filter', () => {
  const defs = [def({ id: 'x', type: 'list', choices }), def({ id: 'b', type: 'bool' }), def({ id: 't', type: 'text' })];
  const pruned = pruneFieldFilter({ x: ['a', 'gone', ''], b: ['yes', 'maybe'], t: [''], deleted: ['a'], y: ['gone'] }, defs);
  assert.deepEqual(pruned, { x: ['a', ''], b: ['yes'] });
  const clean = { x: ['a'], b: ['no'] };
  assert.equal(pruneFieldFilter(clean, defs), clean); // nothing stale: the same object
  assert.deepEqual(pruneFieldFilter({ x: ['gone'] }, defs), {}); // a removed choice alone clears the field
});

test('copyValuesFor keeps values the page does not know and drops known fields that do not apply to the kind', () => {
  const defs = [
    def({ id: 'all', scope: 'copy' }),
    def({ id: 'disc', scope: 'copy', kinds: [3] }),
    def({ id: 'shelf', scope: 'game' }),
  ];
  const values = { all: val('text', 'a'), disc: val('text', 'b'), other: val('text', 'c'), shelf: val('text', 'd') };
  assert.deepEqual(Object.keys(copyValuesFor(values, defs, 1)).sort(), ['all', 'other']);
  assert.deepEqual(Object.keys(copyValuesFor(values, defs, 3)).sort(), ['all', 'disc', 'other']);
  assert.equal(copyValuesFor(values, [], 1).other, values.other); // nothing loaded: nothing is lost
});
