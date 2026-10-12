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
  const a = root.querySelector('a');
  assert.equal(a.textContent, '<b>In</b> your collection');
  assert.equal(labelText(host), '<b>In</b> your collection');
  assert.equal(root.querySelector('b'), null);
  assert.equal(a.getAttribute('target'), '_blank');
  assert.equal(a.getAttribute('rel'), 'noopener noreferrer');
  const { root: other } = ns.renderLabel(window.document, h1, { kind: 'possible', tone: 'possible', text: 'x', href: 'javascript:alert(1)' });
  assert.equal(other.querySelector('a'), null, 'not a web address: no link');
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
