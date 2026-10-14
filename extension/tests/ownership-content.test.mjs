import assert from 'node:assert/strict';
import { test } from 'node:test';
import { countKind, deferred, done, follow, makeT, notOwned, ns, owned, pageWindow, sample, settle } from './helpers.mjs';

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

const byTitle = async (items) => ({ status: 'ok', results: items.map((item) => owned(item.title)) });
const tooltip = (doc) => ns.rootOf(doc.querySelector('[data-gamevault]'))?.querySelector('.gv')?.getAttribute('title') ?? '';

test('a title the store rewrites in place, label kept, is asked about again and its label replaced', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow(sample('fanatical-game.html'), url.current);
  const { sent, ctl } = follow(window, url, byTitle);
  done(tt, window, ctl);
  await settle();
  const h1 = window.document.querySelector('h1.product-name');
  h1.firstChild.data = 'Paper Comet'; // the text node changes; the label's host is untouched
  await settle();
  assert.deepEqual(sent.map((items) => items[0].title), ['Copper Coast', 'Paper Comet']);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
  assert.match(tooltip(window.document), /Paper Comet/);
});

test('a page that never stops changing is still labelled, after the longest wait', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const { sent, ctl } = follow(window, url, byTitle, { debounceMs: 30, maxWaitMs: 80 });
  done(tt, window, ctl);
  const busy = setInterval(() => window.document.body.append(window.document.createElement('i')), 5);
  tt.after(() => clearInterval(busy));
  await settle(250);
  assert.equal(sent.length, 1, 'asked while the page was still changing');
  assert.equal(countKind(window.document, 'owned'), 1);
});

test('stop() disconnects the observer and clears the pending timer', () => {
  const observers = [];
  class FakeObserver {
    constructor(callback) {
      this.callback = callback;
      this.connected = false;
      observers.push(this);
    }

    observe() {
      this.connected = true;
    }

    disconnect() {
      this.connected = false;
    }
  }
  const timers = new Set();
  const ctl = ns.start({
    doc: { documentElement: {} },
    url: () => 'https://example.com/',
    send: async () => undefined,
    t: makeT('en'),
    MutationObserver: FakeObserver,
    setTimer: (fn) => {
      const handle = { fn };
      timers.add(handle);
      return handle;
    },
    clearTimer: (handle) => timers.delete(handle),
  });
  assert.equal(observers.length, 1);
  assert.ok(observers[0].connected);
  assert.equal(timers.size, 1, 'the first scan is scheduled');
  ctl.stop();
  assert.equal(observers[0].connected, false);
  assert.equal(timers.size, 0);
});

test('an answer that arrives after the address changed is dropped', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const answer = deferred();
  const { sent, ctl } = follow(window, url, () => answer.promise);
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  url.current = 'https://store.steampowered.com/';
  answer.resolve({ status: 'ok', results: [owned('Lantern Keep')] });
  await settle();
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0);
  assert.equal(sent.length, 1, 'the new address is no product page: nothing more to ask');
});

test('a scan asked for while one is waiting for Game Vault runs after it, not alongside', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/game/copper-coast' };
  const window = pageWindow(sample('fanatical-game.html'), url.current);
  const answers = [deferred(), deferred()];
  const { sent, ctl } = follow(window, url, () => answers[sent.length - 1].promise);
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  window.document.querySelector('h1.product-name').firstChild.data = 'Paper Comet';
  await ctl.scan();
  await settle();
  assert.equal(sent.length, 1, 'no second lookup while the first is unanswered');
  answers[0].resolve({ status: 'ok', results: [owned('Copper Coast')] });
  await settle();
  assert.deepEqual(sent.map((items) => items[0].title), ['Copper Coast', 'Paper Comet'], 'the waiting scan ran afterwards');
  answers[1].resolve({ status: 'ok', results: [owned('Paper Comet')] });
  await settle();
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
  assert.match(tooltip(window.document), /Paper Comet/);
});

test('a scan that finishes after stop() schedules nothing more', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const answer = deferred();
  let timersAfterStop = 0;
  let stopped = false;
  const { sent, ctl } = follow(window, url, () => answer.promise, {
    setTimer: (fn, ms) => {
      if (stopped) timersAfterStop++;
      return setTimeout(fn, ms);
    },
  });
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  url.current = 'https://store.steampowered.com/app/4411/'; // a rescan would be due
  ctl.stop();
  stopped = true;
  answer.resolve({ status: 'ok', results: [owned('Lantern Keep')] });
  await settle();
  assert.equal(timersAfterStop, 0);
  assert.equal(sent.length, 1);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0, 'no label drawn after stop()');
});

test('a lookup answered after stop(), at the same address, draws nothing', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  const answer = deferred();
  const { sent, ctl } = follow(window, url, () => answer.promise);
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  ctl.stop();
  answer.resolve({ status: 'ok', results: [owned('Lantern Keep')] });
  await settle();
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0, 'no label drawn after stop()');
});

test('scripts left in a page by a reloaded or updated extension stop, and leave the labels to the new ones', async (tt) => {
  const url = { current: 'https://store.steampowered.com/app/4410/Lantern_Keep/' };
  const window = pageWindow(sample('steam-app.html'), url.current);
  let alive = true;
  const { sent, ctl } = follow(window, url, async () => ({ status: 'ok', results: [owned('Lantern Keep')] }), { alive: () => alive });
  done(tt, window, ctl);
  await settle();
  assert.equal(countKind(window.document, 'owned'), 1);

  // The extension was reloaded: these scripts can no longer reach it (nor its translations). A
  // move to another page must not wipe the labels the new scripts draw, nor try to draw any.
  alive = false;
  url.current = 'https://store.steampowered.com/app/4410/Lantern_Keep/?snr=1';
  window.document.body.append(window.document.createElement('div'));
  await settle();
  assert.equal(sent.length, 1);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 1);
});

test('clear() stops following the page and takes every label away (the option turned off)', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map(() => notOwned()) }));
  done(tt, window, ctl);
  await settle();
  assert.ok(window.document.querySelectorAll('[data-gamevault]').length > 0);
  ctl.clear();
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0);
  window.document.body.append(window.document.createElement('div'));
  await settle();
  assert.equal(sent.length, 1);
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0);
});
