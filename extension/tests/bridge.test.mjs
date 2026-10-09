import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import vm from 'node:vm';
import { rememberAllowed } from '../lib/capture.js';

const source = readFileSync(new URL('../bridge.js', import.meta.url), 'utf8');

// loadBridge runs bridge.js in a fake page with a fake extension runtime.
function loadBridge({ connectThrows = false } = {}) {
  const page = { listeners: [], posted: [], intervals: [] };
  const port = { sent: [], onMessage: [], onDisconnect: [], disconnected: false };
  const window = {
    addEventListener: (type, fn) => page.listeners.push(fn),
    postMessage: (data, origin) => page.posted.push({ data, origin }),
  };
  const chrome = {
    runtime: {
      connect: () => {
        if (connectThrows) throw new Error('Extension context invalidated.');
        page.connected = true;
        return {
          postMessage: (m) => port.sent.push(m),
          disconnect: () => { port.disconnected = true; },
          onMessage: { addListener: (fn) => port.onMessage.push(fn) },
          onDisconnect: { addListener: (fn) => port.onDisconnect.push(fn) },
        };
      },
    },
  };
  const context = {
    window, chrome, location: { origin: 'http://127.0.0.1:8093' },
    setInterval: (fn) => { page.intervals.push(fn); return page.intervals.length; },
    clearInterval: (id) => { page.intervals[id - 1] = null; },
  };
  window.window = window;
  vm.runInNewContext(source, context);
  const send = (data, from = { source: window, origin: 'http://127.0.0.1:8093' }) =>
    page.listeners.forEach((fn) => fn({ data, ...from }));
  return { page, port, send, window };
}

const request = { type: 'gamevault-connector', dir: 'request', op: 'connect', id: 'r1', recipe: { version: 1 } };

test('messages from another window or origin are ignored', () => {
  const { page, send } = loadBridge();
  send(request, { source: {}, origin: 'http://127.0.0.1:8093' });
  send(request, { source: undefined, origin: 'https://evil.example' });
  assert.equal(page.connected, undefined);
});

test('a request is relayed, the port is kept alive with pings until the answer, and the answer goes back', () => {
  const { page, port, send } = loadBridge();
  send(request);
  assert.equal(port.sent[0].op, 'connect');
  assert.equal(page.intervals.filter(Boolean).length, 1);
  page.intervals.find(Boolean)();
  assert.equal(port.sent.at(-1).op, 'ping');
  port.onMessage.forEach((fn) => fn({ op: 'result', value: 'v' }));
  assert.equal(page.intervals.filter(Boolean).length, 0, 'pings stop with the answer');
  assert.deepEqual(JSON.parse(JSON.stringify(page.posted.at(-1).data)), { op: 'result', value: 'v', type: 'gamevault-connector', dir: 'response', id: 'r1' });
  assert.equal(page.posted.at(-1).origin, 'http://127.0.0.1:8093');
});

test('a bridge left over from before an extension update answers at once with an error', () => {
  const { page, send } = loadBridge({ connectThrows: true });
  send(request);
  assert.equal(page.posted.at(-1).data.op, 'error');
  assert.equal(page.posted.at(-1).data.code, 'failed');
});

test('remembering a confirmation is offered for https and this computer only', () => {
  assert.equal(rememberAllowed('https://vault.example.com'), true);
  assert.equal(rememberAllowed('http://127.0.0.1:8080'), true);
  assert.equal(rememberAllowed('http://localhost:8080'), true);
  assert.equal(rememberAllowed('http://[::1]:8080'), true);
  assert.equal(rememberAllowed('http://192.168.1.10:8080'), false);
  assert.equal(rememberAllowed('http://nas.local'), false);
});
