import assert from 'node:assert/strict';
import { mock, test } from 'node:test';

// A page with the extension's bridge: it answers hello requests from the given attempt on (a busy
// browser may still be starting the extension when the page first asks).
function page({ answerFrom }) {
  const listeners = new Set();
  let hellos = 0;
  const win = {
    addEventListener: (_, fn) => listeners.add(fn),
    removeEventListener: (_, fn) => listeners.delete(fn),
    setTimeout: (fn, ms) => setTimeout(fn, ms),
    clearTimeout: (t) => clearTimeout(t),
    postMessage(data, origin) {
      if (data.dir !== 'request' || data.op !== 'hello') return;
      hellos++;
      if (hellos < answerFrom) return;
      const reply = { type: data.type, dir: 'response', id: data.id, op: 'hello', version: '1.0.0', recipeVersion: 1 };
      queueMicrotask(() => listeners.forEach((fn) => fn({ source: win, origin, data: reply })));
    },
  };
  globalThis.window = win;
  globalThis.location = { origin: 'http://127.0.0.1:8093' };
  return { hellos: () => hellos };
}

const flush = async () => { for (let i = 0; i < 10; i++) await new Promise((r) => setImmediate(r)); };

test('detect asks a second time when the extension does not answer the first', async () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    const p = page({ answerFrom: 2 });
    const { detect } = await import('../src/lib/connector.ts');
    const found = detect();
    await flush();
    mock.timers.tick(1500);
    await flush();
    mock.timers.tick(1500);
    assert.deepEqual(await found, { version: '1.0.0', recipeVersion: 1 });
    assert.equal(p.hellos(), 2);
  } finally {
    mock.timers.reset();
  }
});
