// The store scripts as the browser starts them in a page: one message listener per world, which
// only this extension's background can use to turn them off. Globals are faked for this file only
// (node --test runs each file in its own process).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { makeT, ns, owned, pageWindow, sample } from './helpers.mjs';

const URL_ = 'https://store.steampowered.com/app/4410/Lantern_Keep/';
const settle = (ms = 700) => new Promise((r) => setTimeout(r, ms)); // the real 400 ms debounce

test('the off message is heard once per page, only from this extension, and a restart answers it again', async (tt) => {
  const window = pageWindow(sample('steam-app.html'), URL_);
  const listeners = [];
  const t = makeT('en');
  Object.assign(globalThis, {
    document: window.document,
    location: { href: URL_ },
    MutationObserver: window.MutationObserver,
    chrome: {
      runtime: {
        id: 'ext',
        onMessage: { addListener: (fn) => listeners.push(fn) },
        sendMessage: async (msg) => ({ status: 'ok', results: msg.items.map(() => owned('Lantern Keep')) }),
      },
      i18n: { getMessage: (key, subs) => t(key, subs) },
    },
  });
  tt.after(async () => {
    for (const k of ['document', 'location', 'MutationObserver', 'chrome', '__gvOwnershipStarted', '__gvOwnershipListening', '__gvOwnershipCtl']) delete globalThis[k];
    await window.happyDOM.close();
  });
  const hosts = () => window.document.querySelectorAll('[data-gamevault]').length;
  const send = (msg, sender) => listeners.forEach((fn) => fn(msg, sender));

  await import('../ownership/content.js?boot=1');
  await settle();
  assert.equal(hosts(), 1);
  assert.equal(listeners.length, 1);

  // Another extension, or a page that found a way to message the scripts: ignored.
  send({ op: 'ownership-off' }, { id: 'other-extension' });
  send({ op: 'something-else' }, { id: 'ext' });
  assert.equal(hosts(), 1);

  send({ op: 'ownership-off' }, { id: 'ext' });
  assert.equal(hosts(), 0);
  assert.equal(globalThis.__gvOwnershipStarted, false);

  // Turned on again: the background puts the scripts back; still one listener, now for them.
  await import('../ownership/content.js?boot=2');
  await settle();
  assert.equal(hosts(), 1);
  assert.equal(listeners.length, 1);
  send({ op: 'ownership-off' }, { id: 'ext' });
  assert.equal(hosts(), 0);
  assert.ok(ns.start);
});
