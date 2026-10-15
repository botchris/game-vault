import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  blockMode, canRefresh, collectionSummary, endsInDays, httpsLink, libraryPreset, minutesUntil, pendingItems, pickValue, refreshDelay,
  staleLine,
} from '../src/lib/home.ts';

const t = (key, opts) => `${key}${opts ? ' ' + JSON.stringify(opts) : ''}`;
const today = new Date(2026, 9, 15, 10, 0, 0); // local time, as the page reads it
const ts = (d) => ({ seconds: BigInt(Math.floor(d.getTime() / 1000)) });

// Copies as the API sends them; every title is invented.
const key = (status, redeemBy = '', redundant = false) => ({ details: { kind: 1, status, redeemBy }, redundant, effectiveSystem: 'PC' });
const library = (system = 'PC') => ({ details: { kind: 2, status: 6, redeemBy: '' }, redundant: false, effectiveSystem: system });
const disc = (system, status = 6) => ({ details: { kind: 3, status, redeemBy: '' }, redundant: false, effectiveSystem: system });
const game = (id, title, copies, extra = {}) => ({ id, title, copies, playStatus: 0, createdAt: ts(new Date(2026, 0, 1)), ...extra });

test('the pending strip counts keys with the library\'s rules, nearest deadline first', () => {
  const games = [
    game('g1', 'Lantern Keep', [key(1, '2026-10-18'), library()]),
    game('g2', 'Echo Valley', [key(2, '2026-11-01', true)]),
    game('g3', 'Harbor Lights', [key(2, '2027-03-01'), key(3, '2026-10-16')]), // the redeemed one is done
    game('g4', 'Copper Coast', [key(1)]),
  ];
  const items = pendingItems(games, [{ level: 'warning' }, { level: 'error' }], today);
  assert.deepEqual(items, [
    { kind: 'expiring', count: 2, nearest: { title: 'Lantern Keep', days: 3, gameId: 'g1' }, href: '#/library/expiring' },
    { kind: 'pending', count: 4, href: '#/library/pending' },
    { kind: 'redundant', count: 1, href: '#/library/redundant' },
    { kind: 'health', count: 2, level: 'error', href: '#/system/health' },
  ]);
});

test('keys past their deadline get their own chip; "expiring" is today up to 30 days', () => {
  const items = pendingItems([
    game('g1', 'Lantern Keep', [key(1, '2026-11-14')]), // 30 days
    game('g2', 'Echo Valley', [key(1, '2026-11-15')]),  // 31 days
    game('g3', 'Harbor Lights', [key(1, '2026-10-12')]), // 3 days ago
    game('g4', 'Copper Coast', [key(1, '2026-10-15')]),  // today
  ], [], today);
  assert.deepEqual(items.slice(0, 2), [
    { kind: 'expired', count: 1, href: '#/library/expiring' },
    { kind: 'expiring', count: 2, nearest: { title: 'Copper Coast', days: 0, gameId: 'g4' }, href: '#/library/expiring' },
  ]);
});

test('with only past deadlines there is no "expiring" chip', () => {
  const items = pendingItems([game('g1', 'Harbor Lights', [key(1, '2026-10-12')])], [], today);
  assert.deepEqual(items.map((it) => it.kind), ['expired', 'pending']);
});

test('with nothing pending, the strip is empty (the page says "All caught up")', () => {
  assert.deepEqual(pendingItems([game('g1', 'Lantern Keep', [library(), key(3)])], [], today), []);
  assert.deepEqual(pendingItems([], [], today), []);
});

test('the collection in figures: held copies, this month in local time, the backlog, the newest, the systems', () => {
  const games = [
    game('g1', 'Lantern Keep', [library(), disc('PS4')], { createdAt: ts(new Date(2026, 9, 1, 0, 30)), playStatus: 1 }),
    game('g2', 'Echo Valley', [disc('Switch'), disc('PS4', 8)], { createdAt: ts(new Date(2026, 8, 30, 23, 30)) }),
    game('g3', 'Harbor Lights', [library()], { createdAt: ts(new Date(2026, 9, 14)), playStatus: 1 }),
  ];
  const s = collectionSummary(games, today);
  assert.equal(s.games, 3);
  assert.equal(s.copies, 4, 'the sold disc is not counted');
  assert.equal(s.addedThisMonth, 2, 'September 30th at 23:30 is last month');
  assert.equal(s.backlog, 2);
  assert.deepEqual(s.recent, ['g3', 'g1', 'g2']);
  assert.deepEqual(s.systems, [
    { system: 'PC', count: 2, rest: false },
    { system: 'PS4', count: 1, rest: false },
    { system: 'Switch', count: 1, rest: false },
  ]);
});

test('an empty collection is all zeros, not an error', () => {
  assert.deepEqual(collectionSummary([], today), { games: 0, copies: 0, addedThisMonth: 0, backlog: 0, recent: [], systems: [] });
});

test('the share by system keeps the five largest and adds the rest up', () => {
  const systems = ['PC', 'PS5', 'PS4', 'Switch', 'Xbox One', 'Wii', 'DS'];
  const games = systems.flatMap((s, i) => Array.from({ length: 7 - i }, (_, j) => game(`${s}-${j}`, `Example ${s} ${j}`, [disc(s)])));
  assert.deepEqual(collectionSummary(games, today).systems, [
    { system: 'PC', count: 7, rest: false },
    { system: 'PS5', count: 6, rest: false },
    { system: 'PS4', count: 5, rest: false },
    { system: 'Switch', count: 4, rest: false },
    { system: 'Xbox One', count: 3, rest: false },
    { system: '', count: 3, rest: true },
  ]);
});

test('"Recently added" keeps the eight newest', () => {
  const games = Array.from({ length: 12 }, (_, i) => game(`g${i}`, `Example ${i}`, [], { createdAt: ts(new Date(2026, 9, 1 + i)) }));
  assert.deepEqual(collectionSummary(games, today).recent, ['g11', 'g10', 'g9', 'g8', 'g7', 'g6', 'g5', 'g4']);
});

test("the collection's value is the first source that priced copies", () => {
  assert.equal(pickValue([]), null);
  assert.equal(pickValue([{ copies: 0, sell: { amountMinor: 0n, currency: 'EUR' } }]), null);
  assert.deepEqual(pickValue([{ copies: 0 }, { copies: 3, sell: { amountMinor: 4500n, currency: 'EUR' } }]), { amountMinor: 4500n, currency: 'EUR' });
});

test('an external block shows its data, a "not configured" card, or a "not fetched yet" card', () => {
  assert.equal(blockMode(undefined), 'notConfigured');
  assert.equal(blockMode({ state: 'not_configured' }), 'notConfigured');
  assert.equal(blockMode({ state: 'never_fetched' }), 'neverFetched');
  assert.equal(blockMode({ state: 'fresh' }), 'data');
  assert.equal(blockMode({ state: 'stale' }), 'data');
});

test('a stale block says how old its data is and why', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  const sixHours = new Date('2026-10-15T06:00:00Z');
  assert.equal(staleLine({ state: 'fresh', errorKind: '', provider: 'IsThereAnyDeal' }, sixHours, now, 'en', t), null);
  assert.equal(staleLine({ state: 'stale', errorKind: 'unavailable', provider: 'IsThereAnyDeal' }, sixHours, now, 'en', t),
    'home.updated {"when":"6 hours ago"} · home.errors.unavailable {"provider":"IsThereAnyDeal"}');
  assert.equal(staleLine({ state: 'stale', errorKind: '', provider: 'IGDB' }, new Date('2026-10-12T12:00:00Z'), now, 'en', t),
    'home.updated {"when":"3 days ago"}', 'old data after a long downtime: no reason to give');
  assert.equal(staleLine({ state: 'stale', errorKind: 'settings', provider: 'IGDB' }, null, now, 'en', t),
    'home.neverUpdated · home.errors.settings {"provider":"IGDB"}');
});

test('a deal ends in whole days; none when it does not say or is over', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(endsInDays(null, now), null);
  assert.equal(endsInDays(new Date('2026-10-15T11:00:00Z'), now), null);
  assert.equal(endsInDays(new Date('2026-10-15T20:00:00Z'), now), 0);
  assert.equal(endsInDays(new Date('2026-10-18T12:00:00Z'), now), 3);
});

test('a block can be refreshed once its 15 minutes are over', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(canRefresh(null, now), true);
  assert.equal(canRefresh(new Date('2026-10-15T12:05:00Z'), now), false);
  assert.equal(canRefresh(new Date('2026-10-15T12:00:00Z'), now), true);
});

test('the home page links to library views by address', () => {
  assert.deepEqual(libraryPreset('pending'), { quick: 'pending' });
  assert.deepEqual(libraryPreset('expiring'), { quick: 'expiring' });
  assert.deepEqual(libraryPreset('redundant'), { quick: 'redundant' });
  assert.deepEqual(libraryPreset('backlog'), { play: 1 });
  assert.deepEqual(libraryPreset('physical'), { kind: 3 });
  assert.deepEqual(libraryPreset('recent'), { sort: 'added' });
  assert.equal(libraryPreset(''), null);
  assert.equal(libraryPreset('anything'), null);
});

test('a card links out only over https, with the address exactly as the service gave it', () => {
  const deal = 'https://itad.link/018d9386-0000-7000-8000-00000000aaaa/?ref=x&tag=y';
  assert.equal(httpsLink(deal), deal);
  assert.equal(httpsLink('HTTPS://www.igdb.com/games/harbor-lights'), 'HTTPS://www.igdb.com/games/harbor-lights');
  assert.equal(httpsLink('http://www.igdb.com/games/harbor-lights'), '');
  assert.equal(httpsLink('javascript:alert(1)'), '');
  assert.equal(httpsLink('//www.igdb.com/games/harbor-lights'), '');
  assert.equal(httpsLink('not a url'), '');
  assert.equal(httpsLink(''), '');
});

test('a disabled Refresh comes back when its time has come', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(refreshDelay(null, now), null, 'never refreshed: enabled now');
  assert.equal(refreshDelay(new Date('2026-10-15T11:59:00Z'), now), null, 'already past');
  assert.equal(refreshDelay(now, now), null, 'exactly now');
  assert.equal(refreshDelay(new Date('2026-10-15T12:07:30Z'), now), 450_000);
});

test('a refresh asked too soon says how many whole minutes are left, at least one', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(minutesUntil(new Date('2026-10-15T12:14:01Z'), now), 15);
  assert.equal(minutesUntil(new Date('2026-10-15T12:14:00Z'), now), 14);
  assert.equal(minutesUntil(new Date('2026-10-15T12:00:10Z'), now), 1);
  assert.equal(minutesUntil(new Date('2026-10-15T11:59:00Z'), now), 1, 'already past: still "in 1 minute"');
  assert.equal(minutesUntil(null, now), 1);
});
