import assert from 'node:assert/strict';
import { test } from 'node:test';
import { countKind, done, follow, notOwned, ns, owned, pageWindow, sample, settle } from './helpers.mjs';

test('a product page gets one label, asked for once, even while the page keeps changing', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const { sent, ctl } = follow(window, url, async () => ({ status: 'ok', results: [owned('Lantern Keep')] }));
  done(tt, window, ctl);
  await settle();
  window.document.body.append(window.document.createElement('div')); // the store's own late changes
  await settle();
  assert.deepEqual(sent, [[{ store: 'steam', id: '4410', title: 'Lantern Keep' }]]);
  assert.equal(countKind(window.document, 'owned'), 1);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
});

test('a single-page store that draws its title late is labelled once the title appears', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow('<!doctype html><html><body><div id="root"></div></body></html>', url.current);
  const { sent, ctl } = follow(window, url, async () => ({ status: 'ok', results: [owned('Copper Coast')] }));
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 0, 'nothing to ask yet');
  window.document.getElementById('root').innerHTML = '<main><h1 class="product-name">Copper Coast</h1></main>';
  await settle();
  assert.deepEqual(sent, [[{ title: 'Copper Coast' }]]);
  assert.ok(ns.hasLabel(window.document.querySelector('h1.product-name')));
});

test('moving to another product without a reload asks again and keeps only the new label', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow(sample('fanatical-game.html'), url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map(notOwned) }));
  done(tt, window, ctl);
  await settle();
  url.current = 'https://www.fanatical.com/en/game/paper-comet';
  window.document.querySelector('h1.product-name').textContent = 'Paper Comet'; // the label's host stays inside the h1
  window.document.body.append(window.document.createElement('div'));
  await settle();
  assert.deepEqual(sent.map((items) => items[0].title), ['Copper Coast', 'Paper Comet']);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
  assert.equal(countKind(window.document, 'not-owned'), 1);
});

test('when Game Vault cannot answer, the label says unavailable, never "not in your collection"', async (tt) => {
  for (const send of [
    async () => ({ status: 'unavailable', reason: 'unreachable' }),
    async () => { throw new Error('the extension was reloaded'); },
    async () => undefined,
  ]) {
    const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
    const window = pageWindow(sample('steam-app.html'), url.current);
    const { ctl } = follow(window, url, send);
    done(tt, window, ctl);
    await settle();
    assert.equal(countKind(window.document, 'unavailable'), 1);
    assert.equal(countKind(window.document, 'not-owned'), 0);
  }
});
