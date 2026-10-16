import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  CoverFailures, NO_FILTERS, activeFilterCount, coverOrder, coverSignature, coverPath, editionsOf, itemLabel, filterItems, groupCopies, itemCounts, libraryItems, mainSystem,
  nextCover, onSeveralSystems, openEdition, severalSystemsCount, sortItems, systemCounts, systemOf,
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

// The cases of TestSystemOf (internal/domain/game/system_test.go): keep both lists the same.
const SYSTEMS = ['PC', 'PS5', 'PS4', 'PS3', 'PS2', 'PS1', 'PSP', 'PS Vita', 'Xbox Series', 'Xbox One', 'Xbox 360', 'Xbox',
  'Switch', 'Wii U', 'Wii', 'GameCube', 'N64', '3DS', 'DS', 'Game Boy'];

const SYSTEM_OF_CASES = [
  ['Steam', 'PC'], ['Epic Games', 'PC'], ['GOG', 'PC'], ['EA App', 'PC'], ['Ubisoft Connect', 'PC'],
  ['Battle.net', 'PC'], ['itch.io', 'PC'], ['Amazon Games', 'PC'], ['Rockstar', 'PC'], ['Riot', 'PC'],
  ['Battlestate (Tarkov)', 'PC'], ['PC', 'PC'], ['steam', 'PC'],
  ['PS5', 'PS5'], ['PS4', 'PS4'], ['PS3', 'PS3'], ['PS2', 'PS2'], ['PS1', 'PS1'], ['PSP', 'PSP'],
  ['PS Vita', 'PS Vita'], ['Xbox Series', 'Xbox Series'], ['Xbox One', 'Xbox One'], ['Xbox 360', 'Xbox 360'],
  ['Xbox', 'Xbox'], ['Switch', 'Switch'], ['Wii U', 'Wii U'], ['Wii', 'Wii'], ['GameCube', 'GameCube'],
  ['N64', 'N64'], ['3DS', '3DS'], ['DS', 'DS'], ['Game Boy', 'Game Boy'], ['xbox 360', 'Xbox 360'],
  ['PlayStation Store', 'PS4'], ['Nintendo eShop', 'Switch'], ['Microsoft Store / Xbox', 'PC'],
  ['  Amiga  ', 'Amiga'], ['Mega Drive', 'Mega Drive'],
  ['', 'Other'], ['   ', 'Other'],
];

for (const [platform, want] of SYSTEM_OF_CASES) {
  test(`systemOf: ${JSON.stringify(platform)} is played on ${want}, as the server says`, () => {
    assert.equal(systemOf(platform, SYSTEMS), want);
  });
}

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

test('a console-only game linked to a PC store borrows the PC box last, and only then', () => {
  const ed = (system, main = false) => ({ system, coverUrl: '', coverPhotoId: '', main });
  const consoleQuest = { editions: [ed('PS4', true)], hasPcStoreLink: true };
  assert.deepEqual(coverOrder(consoleQuest, 'PS4'), ['PS4', 'PC']);
  assert.deepEqual(coverOrder({ ...consoleQuest, editions: [ed('PS4', true), ed('Switch')] }, 'Switch'), ['Switch', 'PS4', 'PC']);
  assert.deepEqual(coverOrder({ ...consoleQuest, hasPcStoreLink: false }, 'PS4'), ['PS4'], 'no PC store: no PC box');
  assert.deepEqual(coverOrder({ ...halo, hasPcStoreLink: true }, 'Xbox 360'), ['Xbox 360', 'PS3', 'PC'], 'its own PC edition, once');
  assert.deepEqual(coverOrder({ editions: [ed('', true)], hasPcStoreLink: true }, ''), [''], 'a game without copies keeps its one address');

  const order = coverOrder(consoleQuest, 'PS4');
  assert.equal(nextCover(order, new Set(['PS4'])), 'PC');
  assert.equal(nextCover(order, new Set(order)), null, 'still stops after the last');
});

test('groupCopies: the current edition first, then in the editions\' order', () => {
  const groups = groupCopies(halo.copies, halo.editions, 'Xbox 360');
  assert.deepEqual(groups.map((g) => [g.system, g.copies.length]), [['Xbox 360', 1], ['PS3', 1], ['PC', 1]]);
});

test('openEdition: the edition asked for while the game has it, else the main one', () => {
  assert.equal(openEdition(halo, 'Xbox 360'), 'Xbox 360');
  assert.equal(openEdition(halo, ''), 'PS3', 'no system (a merge, a move): the main edition');
  assert.equal(openEdition(halo, 'Wii'), 'PS3', 'an edition whose copies moved away: the main one');
  const noCopies = game('g4', 'No copies', [], [edition('', { main: true })]);
  assert.equal(openEdition(noCopies, ''), '', 'a game without copies stays on its only edition');
});

test('in "By platform" an item is named with its system, so two editions of a game differ', () => {
  const t = (key, o) => `${key}:${o.title}|${o.system}`;
  const g = game('g1', 'Halo 3', [copy('PS3', 'PS3', 3), copy('PC', 'Steam', 2)], [edition('PS3', { main: true }), edition('PC')]);
  const [ps3, pc] = libraryItems([g], 'platform');
  assert.equal(itemLabel(t, ps3, 'platform'), 'library.itemLabel:Halo 3|PS3');
  assert.equal(itemLabel(t, pc, 'platform'), 'library.itemLabel:Halo 3|PC');
  assert.equal(itemLabel(t, libraryItems([g], 'game')[0], 'game'), undefined, '"By game" keeps the title');
  assert.equal(itemLabel(t, libraryItems([game('g2', 'Halo 4', [], [])], 'platform')[0], 'platform'), undefined, 'no system, no suffix');
});

test('coverSignature changes with the cover inputs only, not with other edits of the game', () => {
  const base = coverSignature(halo);
  assert.equal(coverSignature({ ...halo, rating: 4, playStatus: 3, updatedAt: { seconds: 99n } }), base, 'a rating or play status keeps it');
  assert.equal(coverSignature({ ...halo, links: {} }), base, 'no links is the same as an empty map');
  assert.equal(coverSignature({ ...halo, links: { steam: '1', gog: '2' } }), coverSignature({ ...halo, links: { gog: '2', steam: '1' } }), 'link order does not matter');

  const changed = {
    title: { ...halo, title: 'Halo 3 ODST' },
    link: { ...halo, links: { steam: '976730' } },
    'chosen cover': { ...halo, editions: [edition('PS3', { main: true, coverUrl: 'https://img/1.jpg' }), edition('PC'), edition('Xbox 360')] },
    'cover photo': { ...halo, editions: [edition('PS3', { main: true }), edition('PC', { coverPhotoId: 'p1' }), edition('Xbox 360')] },
    'main edition': { ...halo, editions: [edition('PS3'), edition('PC', { main: true }), edition('Xbox 360')] },
    'new edition': { ...halo, editions: [...halo.editions, edition('Switch')] },
    'copy platform': { ...halo, copies: [halo.copies[0], copy('PC', 'GOG', 2), halo.copies[2]] },
    'first physical copy in an edition': { ...halo, copies: [...halo.copies, copy('PC', 'Steam', 3)] },
  };
  for (const [what, g] of Object.entries(changed)) assert.notEqual(coverSignature(g), base, what);
  assert.equal(coverSignature({ ...halo, copies: [...halo.copies, copy('PC', 'Steam', 2)] }), base, 'one more copy like one it has keeps it');
});

test('CoverFailures: failures stay until the signature changes, then the cards try again', () => {
  const f = new CoverFailures();
  const order = coverOrder(halo, 'PC');
  const sig = coverSignature(halo);
  assert.equal(nextCover(order, f.failed('g1', sig)), 'PC');

  f.add('g1', sig, 'PC');
  f.add('g1', sig, 'PS3');
  assert.equal(nextCover(order, f.failed('g1', sig)), 'Xbox 360');
  f.add('g1', sig, 'Xbox 360');
  assert.equal(nextCover(order, f.failed('g1', sig)), null, 'all failed: initials, no more requests');

  // An edit that leaves the cover inputs alone (a rating) keeps the record: no new request.
  assert.equal(nextCover(order, f.failed('g1', coverSignature({ ...halo, rating: 5 }))), null);
  assert.equal(nextCover(order, f.failed('g2', sig)), 'PC', 'another game is not affected');

  // A chosen cover starts the game's record over.
  const chosen = coverSignature({ ...halo, editions: [edition('PS3', { main: true }), edition('PC', { coverUrl: 'https://img/pc.jpg' }), edition('Xbox 360')] });
  assert.equal(nextCover(order, f.failed('g1', chosen)), 'PC');
  f.add('g1', chosen, 'PC');
  assert.deepEqual([...f.failed('g1', chosen)], ['PC']);
  assert.equal(f.failed('g1', sig).size, 0, 'the old signature\'s failures are gone');
});

test('CoverFailures: a failure expires after the TTL, so the next render tries once more', () => {
  let now = 1_000;
  const f = new CoverFailures(() => now, 60_000);
  const order = coverOrder(hades, '');
  const sig = coverSignature(hades);
  f.add('g2', sig, 'PC');
  assert.equal(nextCover(order, f.failed('g2', sig)), null, 'failed: initials, no request');

  now += 59_999;
  assert.equal(nextCover(order, f.failed('g2', sig)), null, 'still failed just before the TTL');

  now += 1;
  assert.equal(nextCover(order, f.failed('g2', sig)), 'PC', 'expired: one more try');

  // The retry fails again (a 404, or a 503 while rate-limited): another full TTL before the next one.
  f.add('g2', sig, 'PC');
  now += 30_000;
  assert.equal(nextCover(order, f.failed('g2', sig)), null);
  now += 30_000;
  assert.equal(nextCover(order, f.failed('g2', sig)), 'PC');
});

test('CoverFailures: each system expires on its own clock', () => {
  let now = 0;
  const f = new CoverFailures(() => now, 60_000);
  const order = coverOrder(halo, 'PC');
  const sig = coverSignature(halo);
  f.add('g1', sig, 'PC');
  now = 40_000;
  f.add('g1', sig, 'PS3');
  now = 60_000;
  assert.equal(nextCover(order, f.failed('g1', sig)), 'PC', 'the edition\'s own box is tried again first');
  assert.deepEqual([...f.failed('g1', sig)], ['PS3']);
  now = 100_000;
  assert.equal(f.failed('g1', sig).size, 0);
});

test('"On several platforms": two or more systems, not two stores of one system, never a game without copies', () => {
  const twoConsoles = game('g4', 'Two Consoles', [copy('Xbox 360', 'Xbox 360', 3), copy('PS3', 'PS3', 3)],
    [edition('Xbox 360', { main: true }), edition('PS3')]);
  const pcAndPs4 = game('g5', 'PC and PS4', [copy('PC', 'Steam', 2), copy('PS4', 'PS4', 3)], [edition('PS4', { main: true }), edition('PC')]);
  assert.equal(onSeveralSystems(twoConsoles), true, 'Xbox 360 + PS3');
  assert.equal(onSeveralSystems(pcAndPs4), true, 'PC + PS4');
  assert.equal(onSeveralSystems(hades), false, 'Steam + GOG are both PC');
  assert.equal(onSeveralSystems(empty), false, 'a game without copies');
  assert.equal(onSeveralSystems(game('g6', 'Bare', [], [edition('', { main: true })])), false, 'its one empty edition');

  const all = [halo, hades, empty, twoConsoles, pcAndPs4];
  assert.equal(severalSystemsCount(libraryItems(all, 'platform')), 3 + 2 + 2, 'editions in "By platform"');
  assert.equal(severalSystemsCount(libraryItems(all, 'game')), 3, 'games in "By game"');
});

test('region filter: a copy in one of the regions, counted, empty by default', () => {
  const pal = game('g9', 'Zelda', [copy('Switch', 'Switch', 3, { details: { kind: 3, platform: 'Switch', region: 1, fields: {} } })], [edition('Switch', { main: true })]);
  const items = libraryItems([pal, hades], 'game');
  assert.deepEqual(NO_FILTERS.regions, []);
  assert.deepEqual(keys(items, { regions: [1] }), ['g9']);
  assert.deepEqual(keys(items, { regions: [2] }), []);
  assert.equal(activeFilterCount({ ...NO_FILTERS, regions: [1, 2] }), 2);
});
