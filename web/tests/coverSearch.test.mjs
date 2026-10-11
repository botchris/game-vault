import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import {
  firstImageSrc, googleImagesUrl, imageFromTransfer, isHttpUrl, isSmallPreview, searchTitle, steamGridDbUrl,
} from '../src/lib/coverSearch.ts';

// The cases TestSearchTitle_webAgrees (cmd/gamevault) runs through game.SearchTitle: both sides agree.
const CASES = JSON.parse(readFileSync(new URL('./searchTitle.cases.json', import.meta.url), 'utf8'));

test('searchTitle agrees with game.SearchTitle on the shared cases', () => {
  assert.ok(CASES.length > 0);
  for (const [title, want] of CASES) assert.equal(searchTitle(title), want, title);
});

const query = (url) => new URL(url).searchParams.get('q');

test('the Google Images link searches the cleaned title, the system and "box art", large and tall', () => {
  const url = googleImagesUrl('Example Quest (PS4) Deluxe Edition', 'PS4');
  assert.equal(query(url), '"Example Quest" PS4 box art');
  assert.ok(url.startsWith('https://www.google.com/search?tbm=isch&q='));
  assert.equal(new URL(url).searchParams.get('tbs'), 'isz:l,iar:t');
  assert.equal(new URL(url).searchParams.get('tbm'), 'isch');
});

test('the Google Images link names PC, and no system for a game without copies', () => {
  assert.equal(query(googleImagesUrl('Hades GOG Key', 'PC')), '"Hades" PC box art');
  assert.equal(query(googleImagesUrl('Hades', '')), '"Hades" box art');
});

test('the search links escape the title', () => {
  const g = googleImagesUrl('Ratchet & Clank #1?', 'Xbox 360');
  assert.ok(g.includes('q=%22Ratchet%20%26%20Clank%20%231%3F%22%20Xbox%20360%20box%20art&'));
  assert.equal(query(g), '"Ratchet & Clank #1?" Xbox 360 box art');
  assert.equal(steamGridDbUrl('Ratchet & Clank #1?'), 'https://www.steamgriddb.com/search/grids?term=Ratchet%20%26%20Clank%20%231%3F');
});

test('the SteamGridDB link searches the cleaned title, without a system', () => {
  assert.equal(steamGridDbUrl('Celeste™ (Steam Key)'), 'https://www.steamgriddb.com/search/grids?term=Celeste');
  assert.equal(new URL(steamGridDbUrl('Pokémon™ Épée')).searchParams.get('term'), 'Pokémon Épée');
});

test('only absolute http(s) addresses count', () => {
  assert.equal(isHttpUrl('https://example.com/box.jpg'), true);
  assert.equal(isHttpUrl(' http://example.com/a.png '), true);
  assert.equal(isHttpUrl('ftp://example.com/a.png'), false);
  assert.equal(isHttpUrl('data:image/png;base64,AAAA'), false);
  assert.equal(isHttpUrl('javascript:alert(1)'), false);
  assert.equal(isHttpUrl('example.com/a.png'), false);
  assert.equal(isHttpUrl(''), false);
});

test('Google thumbnails and data: URIs are small previews, other addresses are not', () => {
  assert.equal(isSmallPreview('https://encrypted-tbn0.gstatic.com/images?q=tbn:abc'), true);
  assert.equal(isSmallPreview('https://encrypted-tbn3.gstatic.com/images?q=tbn:abc'), true);
  assert.equal(isSmallPreview('data:image/jpeg;base64,/9j/4AAQ'), true);
  assert.equal(isSmallPreview('DATA:image/png;base64,AAAA'), true);
  assert.equal(isSmallPreview('https://example.com/box.jpg'), false);
  assert.equal(isSmallPreview('https://www.gstatic.com/box.jpg'), false);
  assert.equal(isSmallPreview('https://encrypted-tbn0.gstatic.com.example.com/a.jpg'), false);
  assert.equal(isSmallPreview('not a url'), false);
});

test('firstImageSrc reads the first img src, with any quoting, entities decoded', () => {
  assert.equal(firstImageSrc('<p>x</p><img alt="a" src="https://example.com/a.jpg?x=1&amp;y=2"><img src="https://example.com/b.jpg">'),
    'https://example.com/a.jpg?x=1&y=2');
  assert.equal(firstImageSrc("<IMG SRC='https://example.com/c.png'>"), 'https://example.com/c.png');
  assert.equal(firstImageSrc('<img src=https://example.com/d.png width=10>'), 'https://example.com/d.png');
  assert.equal(firstImageSrc('<img data-src="https://example.com/no.png">'), '');
  assert.equal(firstImageSrc('<a href="https://example.com/">link</a>'), '');
  assert.equal(firstImageSrc(''), '');
});

const transfer = (data) => (type) => data[type] ?? '';

test('a drop is read from text/uri-list first, skipping comments', () => {
  assert.deepEqual(imageFromTransfer(transfer({
    'text/uri-list': '# dragged\r\nhttps://example.com/box.jpg\r\nhttps://example.com/other.jpg',
    'text/plain': 'https://example.com/plain.jpg',
  })), { url: 'https://example.com/box.jpg', small: false });
});

test('a drop without a uri-list address is read from text/plain, then from the html img', () => {
  assert.deepEqual(imageFromTransfer(transfer({ 'text/plain': ' https://example.com/plain.jpg ' })),
    { url: 'https://example.com/plain.jpg', small: false });
  assert.deepEqual(imageFromTransfer(transfer({ 'text/plain': 'Example Quest', 'text/html': '<img src="https://example.com/h.jpg">' })),
    { url: 'https://example.com/h.jpg', small: false });
});

test('a data: URI is skipped for a later http address, and alone only reports a small preview', () => {
  assert.deepEqual(imageFromTransfer(transfer({
    'text/uri-list': 'data:image/jpeg;base64,/9j/4AAQ',
    'text/html': '<img src="https://example.com/full.jpg">',
  })), { url: 'https://example.com/full.jpg', small: false });
  assert.deepEqual(imageFromTransfer(transfer({ 'text/html': '<img src="data:image/jpeg;base64,/9j/4AAQ">' })), { url: '', small: true });
});

test('a Google thumbnail is taken but flagged as a small preview', () => {
  assert.deepEqual(imageFromTransfer(transfer({ 'text/uri-list': 'https://encrypted-tbn0.gstatic.com/images?q=tbn:abc' })),
    { url: 'https://encrypted-tbn0.gstatic.com/images?q=tbn:abc', small: true });
});

test('a drop with no address gives nothing', () => {
  assert.deepEqual(imageFromTransfer(transfer({ 'text/plain': 'just words' })), { url: '', small: false });
  assert.deepEqual(imageFromTransfer(transfer({})), { url: '', small: false });
});
