import assert from 'node:assert/strict';
import { test } from 'node:test';
import { labelText, makeT, ns, pageWindow } from './helpers.mjs';

const t = makeT('en');
const GV = 'http://127.0.0.1:8093';
const game = (title, copies = [], url = `${GV}/#/game/g1`) => ({ title, url, copies });
const disc = (platform) => ({ platform, system: platform, kind: 'physical', pendingKey: false });

/** The CSS a label's shadow root carries: a constructed sheet, or a <style> where those are missing. */
const labelCss = (root) => (root.adoptedStyleSheets?.length
  ? root.adoptedStyleSheets.flatMap((sheet) => [...sheet.cssRules].map((rule) => rule.cssText)).join('\n')
  : root.querySelector('style')?.textContent ?? '');

test('an owned game says so, with each kind of copy once', () => {
  const m = ns.labelModel({ status: 'ok', result: { status: 'owned', candidates: [], game: game('Lantern Keep', [
    disc('PS4'),
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
    { platform: 'GOG', system: 'PC', kind: 'library', pendingKey: false },
  ]) } }, t);
  assert.equal(m.kind, 'owned');
  assert.equal(m.text, 'In your collection: PS4 · disc, Steam · key not redeemed, GOG · library');
  assert.equal(m.href, `${GV}/#/game/g1`);
  assert.equal(m.title, 'Lantern Keep');
});

test('more than three kinds of copy end in +N', () => {
  assert.equal(ns.copiesText(['PS4', 'PS5', 'Switch', 'Xbox One', 'Wii U'].map(disc), t), 'PS4 · disc, PS5 · disc, Switch · disc +2');
});

test('a possible match names its candidates and links to the first', () => {
  const m = ns.labelModel({ status: 'ok', result: { status: 'possible', game: null, candidates: [
    game('Harbor Lights'), game('Harbor Lights (2019)', [], `${GV}/#/game/g2`),
  ] } }, t);
  assert.equal(m.kind, 'possible');
  assert.equal(m.text, 'Possible: Harbor Lights, Harbor Lights (2019)');
  assert.equal(m.href, `${GV}/#/game/g1`);
});

test('not owned is quiet; an unanswered lookup is "unavailable", never "not in your collection"', () => {
  assert.equal(ns.labelModel({ status: 'ok', result: { status: 'not_owned', game: null, candidates: [] } }, t).text, 'Not in your collection');
  for (const reason of ['unreachable', 'token', 'signin', 'setup', 'denied', undefined]) {
    const m = ns.labelModel({ status: 'unavailable', reason }, t);
    assert.equal(m.kind, 'unavailable', String(reason));
    assert.equal(m.text, 'Game Vault unavailable');
    assert.ok(m.title.length > 0, `a hint for ${reason}`);
  }
  assert.equal(ns.labelModel(undefined, t).kind, 'unavailable', 'no answer at all');
  assert.equal(ns.labelModel({ status: 'ok' }, t).kind, 'unavailable', 'an answer without a result');
});

test('labels render in a closed shadow root with their own styles, text as text, and only web links', async (tt) => {
  const window = pageWindow('<!doctype html><html><head><style>span, a { display: none !important; }</style></head><body><h1 id="t">Lantern Keep</h1></body></html>',
    'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const h1 = window.document.getElementById('t');
  const { host, root } = ns.renderLabel(window.document, h1, { kind: 'owned', tone: 'owned', text: '<b>In</b> your collection', href: `${GV}/#/game/g1` });
  assert.equal(host.shadowRoot, null, 'closed: the store page cannot reach into it');
  assert.equal(ns.rootOf(host), root);
  assert.match(host.getAttribute('style'), /all:\s*initial\s*!important/, 'the store page styles do not reach the host');
  assert.match(labelCss(root), /\.gv/);
  assert.equal(h1.textContent, 'Lantern Keep', 'the title the reader sees is unchanged');
  assert.ok(ns.hasLabel(h1));
  const label = root.querySelector('.gv');
  assert.equal(label.textContent, '<b>In</b> your collection');
  assert.equal(labelText(host), '<b>In</b> your collection');
  assert.equal(root.querySelector('b'), null);
  assert.equal(root.querySelector('a'), null, 'no link: a link would be a Tab stop only some answers have');
});

test('the page cannot tell what a label says: its marker is empty and its kind stays with the extension', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const h1 = window.document.getElementById('t');
  const { host } = ns.renderLabel(window.document, h1, { kind: 'owned', tone: 'owned', text: 'In your collection' });
  assert.equal(host.getAttribute('data-gamevault'), '');
  assert.equal(ns.kindOf(host), 'owned');
  assert.doesNotMatch(host.outerHTML, /owned|collection/);
  const { host: mark } = ns.renderLabel(window.document, h1, { kind: 'none' });
  assert.equal(ns.kindOf(mark), 'none');
  assert.equal(mark.getAttribute('style'), host.getAttribute('style'), 'the mark looks like any label from outside');
  assert.equal(ns.kindOf(window.document.createElement('span')), undefined, 'not a label');
});

const models = {
  owned: { kind: 'owned', tone: 'owned', text: 'In your collection: PS4 · disc, Steam · key not redeemed, GOG · library', title: 'Lantern Keep', href: `${GV}/#/game/g1` },
  possible: { kind: 'possible', tone: 'possible', text: 'Possible: Harbor Lights, Harbor Lights (2019)', href: `${GV}/#/game/g1` },
  notOwned: { kind: 'not-owned', tone: 'muted', text: 'Not in your collection' },
  unavailable: { kind: 'unavailable', tone: 'muted', text: 'Game Vault unavailable', title: 'Game Vault did not answer.' },
  none: { kind: 'none' },
};

test('every answer gives a label host of the same declared size, so its size and presence do not tell what the user owns', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const styles = Object.values(models).map((m) => ns.renderLabel(window.document, window.document.getElementById('t'), m).host.getAttribute('style'));
  assert.equal(new Set(styles).size, 1, styles.join('\n'));
  assert.match(styles[0], /(^|;)\s*width:\s*\d+px\s*!important/);
  assert.match(styles[0], /(^|;)\s*height:\s*\d+px\s*!important/);
  assert.doesNotMatch(styles[0], /display:\s*none|visibility:\s*hidden/, 'the "already checked" mark takes the same box');
});

test('the long detail is a tooltip inside the shadow root; the text is cut to fit', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const { host, root } = ns.renderLabel(window.document, window.document.getElementById('t'), models.owned);
  assert.equal(host.getAttribute('title'), null, 'nothing on the host');
  assert.equal(root.querySelector('.gv').getAttribute('title'), `${models.owned.text}\nLantern Keep`);
  assert.match(labelCss(root), /text-overflow:\s*ellipsis/);
});

test('a store rule hiding or resizing spans does not reach the label host', async (tt) => {
  const window = pageWindow('<!doctype html><html><head><style>span { display: none !important; width: 3px !important; height: 0 !important; }</style></head><body><h1 id="t">Lantern Keep</h1></body></html>',
    'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const { host } = ns.renderLabel(window.document, window.document.getElementById('t'), models.owned);
  const computed = window.getComputedStyle(host);
  assert.equal(computed.display, 'inline-block');
  assert.notEqual(computed.width, '3px');
  assert.notEqual(computed.height, '0');
});

test('where constructed style sheets are missing, the label carries a <style> instead', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const { root } = ns.renderLabel(window.document, window.document.getElementById('t'), models.owned);
  assert.equal(root.adoptedStyleSheets.length, 1, 'a constructed sheet, which a page style-src policy does not block');
  assert.equal(root.querySelector('style'), null);
  window.CSSStyleSheet = undefined;
  const { root: fallback } = ns.renderLabel(window.document, window.document.getElementById('t'), models.owned);
  assert.match(fallback.querySelector('style').textContent, /\.gv/);
});

/** The elements of a label's shadow root that take a Tab stop. */
const focusable = (root) => [...root.querySelectorAll('a[href], button, input, select, textarea, [tabindex]')];

test('every visible label takes exactly one Tab stop, of the same element, role and tabindex', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const shapes = ['owned', 'possible', 'notOwned', 'unavailable'].map((name) => {
    const { root } = ns.renderLabel(window.document, window.document.getElementById('t'), models[name]);
    const stops = focusable(root);
    assert.equal(stops.length, 1, name);
    const [el] = stops;
    return JSON.stringify([el.tagName, el.getAttribute('role'), el.getAttribute('tabindex'), el.getAttributeNames().sort()]);
  });
  assert.equal(new Set(shapes).size, 1, shapes.join('\n'));
  assert.deepEqual(JSON.parse(shapes[0]).slice(1, 3), ['button', '0']);
});

test('owned and possible open the game on click or Enter, without an opener; the others do nothing', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  const opened = [];
  window.open = (...args) => {
    opened.push(args);
    return null;
  };
  const press = (name, how) => {
    const el = ns.renderLabel(window.document, window.document.getElementById('t'), models[name]).root.querySelector('[tabindex]');
    if (how === 'click') el.click();
    else el.dispatchEvent(new window.KeyboardEvent('keydown', { key: how, bubbles: true }));
  };
  press('owned', 'click');
  press('possible', 'Enter');
  press('owned', ' ');
  press('owned', 'a');
  assert.deepEqual(opened, [
    [`${GV}/#/game/g1`, '_blank', 'noopener,noreferrer'],
    [`${GV}/#/game/g1`, '_blank', 'noopener,noreferrer'],
    [`${GV}/#/game/g1`, '_blank', 'noopener,noreferrer'],
  ]);
  opened.length = 0;
  press('notOwned', 'click');
  press('unavailable', 'Enter');
  const hostile = ns.renderLabel(window.document, window.document.getElementById('t'), { ...models.owned, href: 'javascript:alert(1)' });
  hostile.root.querySelector('[tabindex]').click();
  assert.deepEqual(opened, [], 'nothing to open, and never a non-web address');
});

test('every visible label handles clicks and keys the same way, so the page cannot hear the answer', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><h1 id="t">Lantern Keep</h1></body></html>', 'https://store.steampowered.com/app/4410/');
  tt.after(() => window.happyDOM.close());
  window.open = () => null;
  const heard = [];
  for (const type of ['click', 'keydown']) window.document.addEventListener(type, (event) => heard.push(event.type));
  const outcomes = ['owned', 'possible', 'notOwned', 'unavailable'].map((name) => {
    const el = ns.renderLabel(window.document, window.document.getElementById('t'), models[name]).root.querySelector('[tabindex]');
    const events = [
      new window.MouseEvent('click', { bubbles: true, composed: true, cancelable: true }),
      new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true, cancelable: true }),
      new window.KeyboardEvent('keydown', { key: ' ', bubbles: true, composed: true, cancelable: true }),
    ];
    for (const event of events) el.dispatchEvent(event);
    return JSON.stringify(events.map((event) => event.defaultPrevented));
  });
  assert.deepEqual(heard, [], 'nothing leaves the label');
  assert.equal(new Set(outcomes).size, 1, outcomes.join('\n'));
  assert.deepEqual(JSON.parse(outcomes[0]), [true, true, true]);
});

test('bundle marks say where a game is owned, or that only a key waits; never a link', () => {
  const answer = (result) => ({ status: 'ok', result });
  const both = ns.markModel(answer({ status: 'owned', candidates: [], game: game('Lantern Keep', [
    disc('PS4'), { platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false },
  ]) }), t);
  assert.equal(both.text, 'Owned on PS4, PC');
  assert.equal(both.href, undefined, 'store cards are often links: no link inside them');
  assert.equal(ns.markModel(answer({ status: 'owned', candidates: [], game: game('Echo Valley', [
    { platform: 'Steam', system: 'PC', kind: 'key', pendingKey: true },
  ]) }), t).text, 'Key not redeemed (PC)');
  assert.equal(ns.markModel(answer({ status: 'possible', game: null, candidates: [game('Harbor Lights')] }), t).text, 'Maybe owned');
  assert.equal(ns.markModel(answer({ status: 'not_owned', game: null, candidates: [] }), t).kind, 'none');
  assert.equal(ns.markModel({ status: 'unavailable', reason: 'unreachable' }, t).kind, 'none');
});

test('the bundle summary counts different titles, possible ones apart, and says unavailable when unanswered', () => {
  const items = ['Lantern Keep', 'Paper Comet', 'lantern keep', 'Harbor Lights'].map((title) => ({ title }));
  const results = [{ status: 'not_owned' }, { status: 'possible' }, { status: 'owned' }, { status: 'not_owned' }];
  const ok = ns.summaryModel({ status: 'ok', results }, items, t);
  assert.equal(ok.kind, 'summary');
  assert.equal(ok.text, 'You own 1 of 3 · 1 possible');
  const down = ns.summaryModel({ status: 'unavailable', reason: 'token' }, items, t);
  assert.equal(down.kind, 'summary');
  assert.equal(down.text, 'Game Vault unavailable');
  assert.match(down.title, /access token/);
});

test('every bundle mark and the summary look and act the same from outside, whatever the answer', async (tt) => {
  const window = pageWindow('<!doctype html><html><body><div id="c"></div></body></html>', 'https://www.humblebundle.com/games/invented-games-bundle');
  tt.after(() => window.happyDOM.close());
  window.open = () => null;
  const heard = [];
  for (const type of ['click', 'keydown']) window.document.addEventListener(type, (event) => heard.push(event.type));
  const ok = (result) => ({ status: 'ok', result });
  const marks = {
    owned: ns.markModel(ok({ status: 'owned', candidates: [], game: game('Lantern Keep', [{ platform: 'Steam', system: 'PC', kind: 'library', pendingKey: false }]) }), t),
    possible: ns.markModel(ok({ status: 'possible', game: null, candidates: [game('Harbor Lights')] }), t),
    notOwned: ns.markModel(ok({ status: 'not_owned', game: null, candidates: [] }), t),
    unavailable: ns.markModel({ status: 'unavailable', reason: 'unreachable' }, t),
    summary: ns.summaryModel({ status: 'ok', results: [{ status: 'owned' }] }, [{ title: 'Lantern Keep' }], t),
  };
  const shapes = Object.entries(marks).map(([name, model]) => {
    const { host, root } = ns.renderLabel(window.document, window.document.getElementById('c'), model);
    const stops = focusable(root);
    assert.equal(stops.length, 1, `${name}: one Tab stop, even when it shows nothing`);
    const events = [
      new window.MouseEvent('click', { bubbles: true, composed: true, cancelable: true }),
      new window.KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true, cancelable: true }),
      new window.KeyboardEvent('keydown', { key: ' ', bubbles: true, composed: true, cancelable: true }),
    ];
    for (const event of events) stops[0].dispatchEvent(event);
    return JSON.stringify([
      host.getAttributeNames().sort(), host.getAttribute('data-gamevault'), host.getAttribute('style'),
      stops[0].tagName, stops[0].getAttribute('role'), stops[0].getAttribute('tabindex'),
      events.map((event) => event.defaultPrevented),
    ]);
  });
  assert.deepEqual(heard, [], 'nothing leaves a mark');
  assert.equal(new Set(shapes).size, 1, shapes.join('\n'));
  assert.equal(labelText(window.document.querySelector('[data-gamevault]')), 'Owned on PC');
  assert.equal(window.document.querySelectorAll('[data-gamevault]')[2].shadowRoot, null);
  assert.equal(labelText(window.document.querySelectorAll('[data-gamevault]')[2]), '', 'the "none" mark is empty inside');
});
