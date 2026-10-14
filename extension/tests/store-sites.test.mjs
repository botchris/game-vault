// Sites that are never a Game Vault: the extension never offers, enables, talks to or sends the
// token to any of them.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { isStoreSite } from '../lib/storesites.js';

// The sites the sign-in recipes open or read (internal/adapters/outbound/*/provider.go), and the
// stores' known sign-in and community sites.
const RECIPE_SITES = [
  'https://www.humblebundle.com/home/keys', 'https://login.live.com/oauth20_authorize.srf', 'https://auth.gog.com/auth',
  'https://embed.gog.com/', 'https://www.epicgames.com/id/login', 'https://connect.ubisoft.com/login',
  'https://www.playstation.com/', 'https://ca.account.sony.com/api/v1/ssocookie', 'https://www.ea.com/',
  'https://accounts.ea.com/connect/auth', 'https://account.battle.net/overview', 'https://www.fanatical.com/en/',
  'https://amazon.com/', 'https://www.amazon.com/ap/signin', 'https://itch.io/login',
];
const KNOWN_SITES = [
  'https://login.steampowered.com/', 'https://store.steampowered.com/app/4410/', 'https://steamcommunity.com/login/home/',
  'https://login.gog.com/', 'https://www.gog.com/en/', 'https://es.humblebundle.com/membership/home#october-2026',
  'https://login.live.com/', 'https://account.xbox.com/', 'https://store.epicgames.com/',
];

test("every store's shop, sign-in, account and community site is a store site", () => {
  for (const url of [...RECIPE_SITES, ...KNOWN_SITES]) assert.equal(isStoreSite(url), true, url);
});

test('a Game Vault on this computer, the network or its own domain is not', () => {
  for (const url of ['http://127.0.0.1:8080/', 'http://192.168.1.20:8080/#/library', 'https://vault.example.com/', 'https://gamevault.tailnet.ts.net/']) {
    assert.equal(isStoreSite(url), false, url);
  }
  // Only a whole label matches: a look-alike domain is not the store, nor is it trusted for it.
  assert.equal(isStoreSite('https://notgog.com/'), false);
  assert.equal(isStoreSite('not an address'), false);
});
