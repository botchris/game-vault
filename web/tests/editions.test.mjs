import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  NO_FILTERS, activeFilterCount, coverOrder, coverPath, editionsOf, filterItems, groupCopies, itemCounts, libraryItems, mainSystem,
  nextCover, sortItems, systemCounts, systemOf,
} from '../src/lib/editions.ts';

// Plain objects shaped like the generated messages; kinds: 1 key, 2 library, 3 physical.
const edition = (system, over = {}) => ({ system, coverUrl: '', coverPhotoId: '', main: false, ...over });
const copy = (system, platform, kind, over = {}) => ({ effectiveSystem: system, sourceId: '', redundant: false, details: { kind, platform, fields: {} }, ...over });
const game = (id, title, copies, editions, over = {}) => ({ id, title, genres: [], playStatus: 0, fields: {}, copies, editions, ...over });

const halo = game('g1', 'Halo 3', [
  copy('PS3', 'PS3', 3),
  copy('PC', 'Steam', 2),
  copy('Xbox 360', 'Xbox 360', 1, { sourceId: 's1', redundant: true }),
], [edition('PS3', { main: true }), edition('PC'), edition('Xbox 360')], { genres: ['Shooter'], playStatus: 2 });
const hades = game('g2', 'Hades', [copy('PC', 'Steam', 2), copy('PC', 'GOG', 2)], [edition('PC', { main: true })]);
const empty = game('g3', 'Empty', [], []);

const keys = (items, f = {}, checks) => filterItems(items, { ...NO_FILTERS, ...f }, [], checks).map((i) => i.key);

test('coverPath escapes the system, and without one is the main edition address', () => {
  assert.equal(coverPath('g1', 'Xbox 360'), '/media/covers/g1/Xbox%20360');
  assert.equal(coverPath('g1', 'Xbox 360/S Slim'), '/media/covers/g1/Xbox%20360%2FS%20Slim');
  assert.equal(coverPath('g1', ''), '/media/covers/g1');
});

test('mainSystem is the edition marked main, or none without editions', () => {
  assert.equal(mainSystem(halo), 'PS3');
  assert.equal(mainSystem(empty), '');
});

test('systemOf mirrors the server', () => {
  assert.equal(systemOf('Steam'), 'PC');
  assert.equal(systemOf('microsoft store / xbox'), 'PC');
  assert.equal(systemOf('PlayStation Store'), 'PS4');
  assert.equal(systemOf('Nintendo eShop'), 'Switch');
  assert.equal(systemOf('xbox 360'), 'Xbox 360');
  assert.equal(systemOf(' Amiga '), 'Amiga');
  assert.equal(systemOf(''), 'Other');
});

test('editionsOf: one item per edition holding its copies; a game without copies is one item', () => {
  const items = editionsOf(halo);
  assert.deepEqual(items.map((i) => i.key), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(items[1].view.copies.map((c) => c.details.platform), ['Steam']);
  assert.equal(items[1].game, halo, 'the item keeps the whole game');
  assert.deepEqual(editionsOf(empty).map((i) => [i.key, i.system]), [['g3', '']]);
});

test('libraryItems: by game, one item per game on its main edition', () => {
  const items = libraryItems([halo, hades, empty], 'game');
  assert.deepEqual(items.map((i) => [i.key, i.system]), [['g1', 'PS3'], ['g2', 'PC'], ['g3', '']]);
  assert.equal(items[0].view, halo);
  assert.deepEqual(itemCounts(libraryItems([halo, hades], 'platform')), { items: 4, games: 2 });
  assert.deepEqual(itemCounts(items), { items: 3, games: 3 });
});

test('"By platform": copy-level filters keep an edition when one of its copies matches', () => {
  const items = libraryItems([halo, hades], 'platform');
  assert.deepEqual(keys(items, { kind: 1 }), ['g1|Xbox 360']);
  assert.deepEqual(keys(items, { platforms: ['Steam'] }), ['g1|PC', 'g2|PC']);
  assert.deepEqual(keys(items, { sources: ['s1'] }), ['g1|Xbox 360']);
  assert.deepEqual(keys(items, {}, { copyLevel: (v) => v.copies.some((c) => c.redundant) }), ['g1|Xbox 360']);
});

test('game-level filters and the search apply to all of a game\'s editions', () => {
  const items = libraryItems([halo, hades], 'platform');
  assert.deepEqual(keys(items, { genres: ['Shooter'] }), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(keys(items, { play: [2] }), ['g1|PS3', 'g1|PC', 'g1|Xbox 360']);
  assert.deepEqual(keys(items, {}, { gameLevel: (g) => g.copies.some((c) => c.details.platform === 'GOG') }), ['g2|PC']);
});

test('"By game": filters look at all of the game\'s copies', () => {
  const items = libraryItems([halo, hades], 'game');
  assert.deepEqual(keys(items, { kind: 1 }), ['g1']);
  assert.deepEqual(keys(items, { platforms: ['Steam'] }), ['g1', 'g2']);
  assert.deepEqual(keys(items, {}, { copyLevel: (v) => v.copies.some((c) => c.redundant) }), ['g1']);
});

test('the System filter matches editions in "By platform" and games in "By game"', () => {
  assert.deepEqual(keys(libraryItems([halo, hades], 'platform'), { systems: ['PS3', 'Xbox 360'] }), ['g1|PS3', 'g1|Xbox 360']);
  assert.deepEqual(keys(libraryItems([halo, hades], 'game'), { systems: ['PS3', 'Xbox 360'] }), ['g1']);
  assert.equal(activeFilterCount({ ...NO_FILTERS, systems: ['PS3', 'PC'] }), 2);
});

test('System options are counted by editions in "By platform" and by games in "By game", most first', () => {
  const ps3Twice = game('g4', 'Ico', [copy('PS3', 'PS3', 3), copy('PS3', 'PS3', 3)], [edition('PS3', { main: true })]);
  const views = (grouping) => libraryItems([halo, hades, ps3Twice], grouping).map((i) => i.view);
  assert.deepEqual(systemCounts(views('platform')), [['PC', 2], ['PS3', 2], ['Xbox 360', 1]]);
  assert.deepEqual(systemCounts(views('game')), [['PC', 2], ['PS3', 2], ['Xbox 360', 1]]);
  assert.deepEqual(systemCounts([halo]), [['PC', 1], ['PS3', 1], ['Xbox 360', 1]], 'a game counts once per system');
});

test('sorting breaks ties by system, and the copies sort counts the edition\'s copies', () => {
  const items = libraryItems([halo, hades], 'platform');
  const byTitle = (a, b) => a.title.localeCompare(b.title);
  assert.deepEqual(sortItems(items, byTitle).map((i) => i.key), ['g2|PC', 'g1|PC', 'g1|PS3', 'g1|Xbox 360']);
  const byCopies = (a, b) => b.copies.length - a.copies.length;
  assert.deepEqual(sortItems(items, byCopies).map((i) => i.key), ['g2|PC', 'g1|PC', 'g1|PS3', 'g1|Xbox 360']);
});

test('a card tries its own cover, then the main edition\'s, then the others\', and stops after the last', () => {
  assert.deepEqual(coverOrder(halo, 'Xbox 360'), ['Xbox 360', 'PS3', 'PC']);
  assert.deepEqual(coverOrder(halo, 'PS3'), ['PS3', 'PC', 'Xbox 360']);
  assert.deepEqual(coverOrder(halo, ''), ['PS3', 'PC', 'Xbox 360']);
  assert.deepEqual(coverOrder(empty, ''), ['']);

  const order = coverOrder(halo, 'Xbox 360');
  assert.equal(nextCover(order, new Set()), 'Xbox 360');
  assert.equal(nextCover(order, new Set(['Xbox 360'])), 'PS3');
  assert.equal(nextCover(order, new Set(order)), null, 'every cover failed: initials, and no more requests');
});

test('groupCopies: the current edition first, then in the editions\' order', () => {
  const groups = groupCopies(halo.copies, halo.editions, 'Xbox 360');
  assert.deepEqual(groups.map((g) => [g.system, g.copies.length]), [['Xbox 360', 1], ['PS3', 1], ['PC', 1]]);
});
