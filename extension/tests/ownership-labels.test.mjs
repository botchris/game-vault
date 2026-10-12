import assert from 'node:assert/strict';
import { test } from 'node:test';
import { labelText, makeT, ns, pageWindow } from './helpers.mjs';

const t = makeT('en');
const GV = 'http://127.0.0.1:8093';
const game = (title, copies = [], url = `${GV}/#/game/g1`) => ({ title, url, copies });
const disc = (platform) => ({ platform, system: platform, kind: 'physical', pendingKey: false });

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
  assert.match(host.getAttribute('style'), /all:\s*initial/, 'the store page styles do not reach the host');
  assert.match(root.querySelector('style').textContent, /\.gv/);
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
  assert.match(mark.getAttribute('style'), /display:\s*none/);
  assert.equal(ns.kindOf(window.document.createElement('span')), undefined, 'not a label');
});
