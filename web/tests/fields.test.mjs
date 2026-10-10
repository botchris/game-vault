import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fieldText, filterOptions, matchesFieldFilters, numberInput, parseNumber, searchableText } from '../src/lib/fields.ts';

const fmt = {
  money: (n, c) => `${c} ${n}`,
  date: (d) => `<${d}>`,
  yes: 'Yes', no: 'No', hours: 'h', minutes: 'min',
};
const def = (over) => ({ id: 'f', name: 'F', type: 'text', scope: 'game', kinds: [], decimals: 0, unit: '', currency: '', choices: [], ...over });
const val = (c, v) => ({ value: { case: c, value: v } });
const choices = [{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Beta' }, { id: 'c', name: 'Gamma' }];
const game = (fields, copies = []) => ({ fields, copies: copies.map((f) => ({ details: { kind: 3, fields: f } })) });

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

test('numberInput writes hundredths with two decimals', () => {
  assert.equal(numberInput(1250n, 2), '12.50');
  assert.equal(numberInput(-5n, 2), '-0.05');
  assert.equal(numberInput(42n, 0), '42');
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
