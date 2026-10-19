import assert from 'node:assert/strict';
import { test } from 'node:test';
import { saveAndRequest } from '../lib/permission.js';

// A browser whose storage answers late, as it does, recording the order of the calls.
function fakeBrowser({ grant = true } = {}) {
  const calls = [];
  let finishWrite;
  const chrome = {
    storage: { local: { set: (items) => { calls.push(['set', items]); return new Promise((r) => { finishWrite = r; }); } } },
    permissions: { request: (req) => { calls.push(['request', req]); return Promise.resolve(grant); } },
  };
  return { chrome, calls, finishWrite: () => finishWrite() };
}

test('the permission is asked for in the same turn as the click, after the write has started', () => {
  const b = fakeBrowser();
  saveAndRequest(b.chrome, { pendingEnable: { origin: 'https://vault.example' } }, { origins: ['https://vault.example/*'] });
  // Nothing was awaited: Firefox only shows its prompt while it is still handling the click.
  assert.deepEqual(b.calls, [
    ['set', { pendingEnable: { origin: 'https://vault.example' } }],
    ['request', { origins: ['https://vault.example/*'] }],
  ]);
});

test('the answer is the browser\'s, once the write is done too', async () => {
  const granted = fakeBrowser({ grant: true });
  const p = saveAndRequest(granted.chrome, { a: 1 }, { origins: [] });
  granted.finishWrite();
  assert.equal(await p, true);

  const refused = fakeBrowser({ grant: false });
  const q = saveAndRequest(refused.chrome, { a: 1 }, { origins: [] });
  refused.finishWrite();
  assert.equal(await q, false);
});
