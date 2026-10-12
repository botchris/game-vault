// Each store's reader against a saved page. When a store changes its markup, fix the selectors in
// ownership/stores.js and update the sample here.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ns, pageWindow, sample } from './helpers.mjs';

const products = [
  { file: 'steam-app.html', url: 'https://store.steampowered.com/app/4410/Lantern_Keep/', item: { store: 'steam', id: '4410', title: 'Lantern Keep' } },
  { file: 'gog-game.html', url: 'https://www.gog.com/en/game/echo_valley', item: { store: 'gog', id: '1207664663', title: 'Echo Valley' } },
  { file: 'humble-store.html', url: 'https://www.humblebundle.com/store/harbor-lights', item: { title: 'Harbor Lights' } },
  { file: 'fanatical-game.html', url: 'https://www.fanatical.com/en/game/copper-coast', item: { title: 'Copper Coast' } },
];

for (const p of products) {
  test(`${p.file}: the product page gives its item and the element its label goes in`, async (t) => {
    const window = pageWindow(sample(p.file), p.url);
    t.after(() => window.happyDOM.close());
    const found = ns.pageFor(p.url);
    assert.ok(found, 'a known store page');
    const read = ns.readPage(window.document, found);
    assert.equal(read.kind, 'product');
    assert.deepEqual(read.items.map((e) => e.item), [p.item]);
    assert.match(read.items[0].element.textContent, new RegExp(p.item.title));
  });
}

test('other pages of the stores, and other sites, are left alone', () => {
  for (const url of [
    'https://store.steampowered.com/',
    'https://store.steampowered.com/agecheck/app/4410/',
    'https://www.gog.com/en/games',
    'https://example.com/app/4410',
    'https://store.steampowered.com.example.com/app/4410/',
    'not an address',
  ]) assert.equal(ns.pageFor(url), null, url);
});

test('a product page that has not drawn its title yet reads as nothing, for now', async (t) => {
  const url = 'https://www.fanatical.com/en/game/copper-coast';
  const window = pageWindow('<!doctype html><html><body><div id="root"></div></body></html>', url);
  t.after(() => window.happyDOM.close());
  assert.equal(ns.readPage(window.document, ns.pageFor(url)), null);
});

test('a GOG product id that is not a number is not sent; the title still is', async (t) => {
  const url = 'https://www.gog.com/en/game/echo_valley';
  const window = pageWindow('<!doctype html><html><body><div card-product="{{ id }}"><h1 class="productcard-basics__title">Echo Valley</h1></div></body></html>', url);
  t.after(() => window.happyDOM.close());
  assert.deepEqual(ns.readPage(window.document, ns.pageFor(url)).items[0].item, { title: 'Echo Valley' });
});

const bundles = [
  {
    file: 'humble-bundle.html',
    url: 'https://www.humblebundle.com/games/invented-games-bundle',
    titles: ['Lantern Keep', 'Paper Comet', 'Harbor Lights', 'Night Ferry', 'Lantern Keep'],
    summary: 'Invented Games Bundle',
  },
  {
    file: 'humble-choice.html',
    url: 'https://es.humblebundle.com/membership/home#invented-month/invented-game',
    titles: ['Lantern Keep', 'Paper Comet', 'Harbor Lights'],
    summary: 'Invented Month games',
  },
  {
    file: 'fanatical-bundle.html',
    url: 'https://www.fanatical.com/en/bundle/invented-mystery-bundle',
    titles: ['Echo Valley', 'Copper Coast', 'Glass Orchard'],
    summary: 'Invented Mystery Bundle',
  },
];

for (const b of bundles) {
  test(`${b.file}: the bundle page gives each game and the heading its summary goes in`, async (t) => {
    const window = pageWindow(sample(b.file), b.url);
    t.after(() => window.happyDOM.close());
    const read = ns.readPage(window.document, ns.pageFor(b.url));
    assert.equal(read.kind, 'bundle');
    assert.deepEqual(read.items.map((e) => e.item), b.titles.map((title) => ({ title })));
    assert.equal(ns.textOf(read.summary), b.summary);
  });
}

test("Humble's language sites are the same store", () => {
  for (const url of ['https://es.humblebundle.com/store/invented-game?hmb_source=search_bar', 'https://humblebundle.com/store/invented-game', 'https://de.humblebundle.com/games/invented-bundle']) {
    assert.equal(ns.pageFor(url)?.store.store, 'humble', url);
  }
  for (const url of ['https://humblebundle.com.example/store/invented-game', 'https://eshumblebundle.com/store/invented-game']) {
    assert.equal(ns.storeForUrl(url), null, url);
  }
});

test('Fanatical pick-and-mix pages are bundles too', () => {
  assert.equal(ns.pageFor('https://www.fanatical.com/en/pick-and-mix/invented-build-your-own').page.kind, 'bundle');
  assert.equal(ns.pageFor('https://www.fanatical.com/es/bundle/invented-mystery-bundle').page.kind, 'bundle');
});
