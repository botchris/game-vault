// What the popup offers for the tab it is opened on: the Game Vault bridge only on an address that
// could be the user's Game Vault, never on a store page or a store's sign-in site.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { popupPlace } from '../lib/ownership.js';

const GV = 'http://127.0.0.1:8093';
const grants = { [GV]: ['account.battle.net', 'www.humblebundle.com'] };

test('an address that may be a Game Vault is offered; an enabled one says so', () => {
  assert.deepEqual(popupPlace('http://192.168.1.20:8080/#/library', [GV], grants), { kind: 'offer', origin: 'http://192.168.1.20:8080' });
  assert.deepEqual(popupPlace(`${GV}/#/library`, [GV], grants), { kind: 'enabled', origin: GV });
});

test('a store page is never offered the bridge: the popup speaks about its labels instead', () => {
  for (const [url, name] of [
    ['https://store.steampowered.com/app/4410/', 'Steam'],
    ['https://es.humblebundle.com/membership/home#october-2026', 'Humble Bundle'],
    ['https://www.gog.com/en/game/echo_valley', 'GOG'],
    ['https://www.fanatical.com/en/game/copper-coast', 'Fanatical'],
  ]) assert.deepEqual(popupPlace(url, [GV], grants), { kind: 'store', origin: new URL(url).origin, store: name }, url);
});

test("a store's sign-in site, confirmed for a sign-in, is not offered either", () => {
  assert.deepEqual(popupPlace('https://account.battle.net/overview', [GV], grants), { kind: 'signin', origin: 'https://account.battle.net' });
});

test('pages that are not on the web offer nothing', () => {
  for (const url of ['chrome://extensions/', 'about:blank', '', 'not an address']) {
    assert.deepEqual(popupPlace(url, [GV], grants), { kind: 'none' }, url);
  }
});
