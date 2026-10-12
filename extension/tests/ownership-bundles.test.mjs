import assert from 'node:assert/strict';
import { test } from 'node:test';
import { countKind, done, follow, labelText, notOwned, ns, owned, pageWindow, sample, settle } from './helpers.mjs';

const possible = (title) => ({ status: 'possible', game: null, candidates: [{ title, url: 'http://127.0.0.1:8093/#/game/g2', copies: [] }] });

/** The summary's host (the only label of that kind) and its text. */
const summaryHost = (window) => [...window.document.querySelectorAll('[data-gamevault]')].find((host) => ns.kindOf(host) === 'summary');
const summaryText = (window) => labelText(summaryHost(window));

test('a Humble bundle gets a mark on every game and one summary', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({
    status: 'ok',
    results: items.map((it) => (it.title === 'Lantern Keep' ? owned(it.title) : it.title === 'Harbor Lights' ? possible(it.title) : notOwned())),
  }));
  done(tt, window, ctl);
  await settle();
  assert.equal(sent.length, 1);
  assert.equal(sent[0].length, 5);
  assert.equal(countKind(window.document, 'owned'), 2, 'the game listed in two tiers is marked in both');
  assert.equal(countKind(window.document, 'possible'), 1);
  assert.equal(countKind(window.document, 'none'), 2);
  assert.equal(countKind(window.document, 'summary'), 1);
  assert.equal(summaryText(window), 'You own 1 of 4 · 1 possible');
  for (const card of window.document.querySelectorAll('.tier-item-view')) {
    assert.equal([...card.children].filter((c) => c.hasAttribute('data-gamevault')).length, 1, 'exactly one mark in every card, owned or not');
  }
});

test('games a bundle page adds later are marked too, and the summary counts them', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/pick-and-mix/invented-build-your-own' };
  const window = pageWindow(sample('fanatical-bundle.html'), url.current);
  const mine = new Set(['Echo Valley', 'Paper Comet']);
  const { sent, ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map((it) => (mine.has(it.title) ? owned(it.title) : notOwned())) }));
  done(tt, window, ctl);
  await settle();
  assert.equal(summaryText(window), 'You own 1 of 3');
  window.document.querySelector('.bundle-games').insertAdjacentHTML('beforeend',
    '<div class="bundle-game-card"><h3 class="bundle-game-card__title">Paper Comet</h3></div>'
    + '<div class="bundle-game-card"><h3 class="bundle-game-card__title">Night Ferry</h3></div>');
  await settle();
  assert.equal(sent.length, 2);
  assert.equal(sent[1].length, 5, 'the summary counts the whole bundle');
  assert.equal(summaryText(window), 'You own 2 of 5');
  assert.equal(countKind(window.document, 'summary'), 1);
  assert.equal(countKind(window.document, 'owned'), 2);
});

test('a bundle of 450 games gets a mark on each and one summary, from one message', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-mega-bundle' };
  const cards = Array.from({ length: 450 }, (_, i) => `<div class="tier-item-view"><span class="item-title">Invented Game ${i + 1}</span></div>`).join('');
  const window = pageWindow(`<!doctype html><html><body><h1>Invented Mega Bundle</h1>${cards}</body></html>`, url.current);
  const { sent, ctl } = follow(window, url, async (items) => ({
    status: 'ok',
    results: items.map((it) => (it.title === 'Invented Game 450' ? owned(it.title) : notOwned())),
  }));
  done(tt, window, ctl);
  await settle(300);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].length, 450);
  const marked = [...window.document.querySelectorAll('.tier-item-view')].filter((c) => ns.hasLabel(c)).length;
  assert.equal(marked, 450);
  assert.equal(countKind(window.document, 'owned'), 1);
  assert.equal(summaryText(window), 'You own 1 of 450');
});

test('when Game Vault cannot answer, a bundle says so in its summary and marks nothing as owned', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  const { ctl } = follow(window, url, async () => ({ status: 'unavailable', reason: 'unreachable' }));
  done(tt, window, ctl);
  await settle();
  assert.equal(summaryText(window), 'Game Vault unavailable');
  assert.equal(countKind(window.document, 'owned') + countKind(window.document, 'possible'), 0);
  assert.equal(countKind(window.document, 'none'), 5, 'checked, invisibly: not asked again on every change');
});

test('a bundle answered after stop() draws nothing', async (tt) => {
  const url = { current: 'https://www.humblebundle.com/games/invented-games-bundle' };
  const window = pageWindow(sample('humble-bundle.html'), url.current);
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  const { ctl } = follow(window, url, async (items) => {
    await gate;
    return { status: 'ok', results: items.map(() => notOwned()) };
  });
  done(tt, window, ctl);
  await settle();
  ctl.stop();
  release();
  await settle();
  assert.equal(window.document.querySelectorAll('[data-gamevault]').length, 0);
});

test('a bundle mark whose card now shows another game is replaced', async (tt) => {
  const url = { current: 'https://www.fanatical.com/en/bundle/invented-mystery-bundle' };
  const window = pageWindow(sample('fanatical-bundle.html'), url.current);
  const mine = new Set(['Night Ferry']);
  const { ctl } = follow(window, url, async (items) => ({ status: 'ok', results: items.map((it) => (mine.has(it.title) ? owned(it.title) : notOwned())) }));
  done(tt, window, ctl);
  await settle();
  assert.equal(countKind(window.document, 'owned'), 0);
  window.document.querySelector('.bundle-game-card__title').textContent = 'Night Ferry';
  await settle();
  assert.equal(countKind(window.document, 'owned'), 1);
  assert.equal(countKind(window.document, 'summary'), 1);
  assert.equal(summaryText(window), 'You own 1 of 3');
});
