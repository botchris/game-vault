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
      const reply = { type: data.type, dir: 'response', id: data.id, op: 'hello', version: '1.0.0', recipeVersion: 1, privateAllowed: true };
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
    assert.deepEqual(await found, { version: '1.0.0', recipeVersion: 1, privateAllowed: true });
    assert.equal(p.hellos(), 2);
  } finally {
    mock.timers.reset();
  }
});

test('the page waits for a sign-in as long as the recipe allows, plus a minute', async () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    page({ answerFrom: Infinity }); // the extension never answers the connect
    const { connect } = await import('../src/lib/connector.ts');
    const run = connect('humble', 'cookie', { version: 1, open: 'https://www.humblebundle.com/', timeoutSeconds: 120 });
    let outcome = 'pending';
    run.result.catch((e) => { outcome = e.code; });
    mock.timers.tick(180_000 - 1);
    await flush();
    assert.equal(outcome, 'pending');
    mock.timers.tick(1);
    await flush();
    assert.equal(outcome, 'timeout');
  } finally {
    mock.timers.reset();
  }
});

test('without a timeout in the recipe the page uses the extension default of five minutes, plus a minute', async () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    page({ answerFrom: Infinity });
    const { connect } = await import('../src/lib/connector.ts');
    const run = connect('humble', 'cookie', { version: 1, open: 'https://www.humblebundle.com/' });
    let outcome = 'pending';
    run.result.catch((e) => { outcome = e.code; });
    mock.timers.tick(360_000);
    await flush();
    assert.equal(outcome, 'timeout');
  } finally {
    mock.timers.reset();
  }
});
