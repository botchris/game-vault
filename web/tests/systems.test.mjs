import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { findSystem, groupByMaker, pickerGroups, systemKey } from '../src/lib/systems.ts';

const sys = (name, maker, aliases = [], kind = 'home') => ({ name, maker, kind, year: 2000, aliases, short: '' });
const catalog = [
  sys('PC', 'PC', ['Steam Deck'], 'pc'),
  sys('PS4', 'Sony', ['PlayStation 4']),
  sys('SNES', 'Nintendo', ['Super Nintendo', 'Super Famicom', 'SFC']),
  sys('NES', 'Nintendo', ['Famicom']),
  sys('Mega Drive / Genesis', 'Sega', ['Mega Drive', 'Genesis', 'Sega Genesis']),
];

test('systemKey agrees with the server on the shared cases', () => {
  for (const [input, want] of JSON.parse(readFileSync(new URL('./systemKey.cases.json', import.meta.url), 'utf8'))) {
    assert.equal(systemKey(input), want, input);
  }
});

test('findSystem finds by name or alias, ignoring case, spaces, hyphens and dots', () => {
  assert.equal(findSystem(catalog, 'super-famicom')?.name, 'SNES');
  assert.equal(findSystem(catalog, 'p.s. 4')?.name, 'PS4');
  assert.equal(findSystem(catalog, 'Amiga'), undefined);
});

test('without a query: the user\'s systems first, then every system by maker', () => {
  const groups = pickerGroups(catalog, ['SNES', 'Commodore PET'], '');
  assert.deepEqual(groups.map((g) => g.maker), [null, 'PC', 'Sony', 'Nintendo', 'Sega']);
  assert.deepEqual(groups[0].options.map((o) => o.name), ['SNES', 'Commodore PET']);
});

test('a query filters by name and alias, and shows the alias that matched', () => {
  const groups = pickerGroups(catalog, ['SNES'], 'famicom');
  assert.deepEqual(groups.map((g) => g.maker), ['Nintendo']);
  assert.deepEqual(groups[0].options, [
    { name: 'SNES', alias: 'Super Famicom', maker: 'Nintendo' },
    { name: 'NES', alias: 'Famicom', maker: 'Nintendo' },
  ]);
});

test('a query matching a name shows no alias', () => {
  assert.deepEqual(pickerGroups(catalog, [], 'ps4')[0].options, [{ name: 'PS4', maker: 'Sony' }]);
});

test('no match gives no groups (the picker offers the typed text)', () => {
  assert.deepEqual(pickerGroups(catalog, [], 'amiga'), []);
});

test('groupByMaker groups by maker in catalog order and puts unknown names last', () => {
  const groups = groupByMaker(['Mega Drive', 'Zeebo', 'NES', 'PS4', 'SNES'], catalog);
  assert.deepEqual(groups, [
    { maker: 'Sony', names: ['PS4'] },
    { maker: 'Nintendo', names: ['SNES', 'NES'] },
    { maker: 'Sega', names: ['Mega Drive'] },
    { maker: '', names: ['Zeebo'] },
  ]);
});
